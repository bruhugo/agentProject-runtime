package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/bruhugo/PicoClawProjectRuntime/include/blobstorage"
	"github.com/bruhugo/PicoClawProjectRuntime/include/config"
	"github.com/bruhugo/PicoClawProjectRuntime/include/types"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

type DockerTemplate interface {
	Start(ctx context.Context) error
	Close()

	PullPicoclawImage(ctx context.Context) error

	CreateAgentContainer(ctx context.Context, agent *types.Agent) (*types.Container, error)
	StartAgentContainer(ctx context.Context, agent *types.Agent, container *types.Container) error

	FindByUserID(ctx context.Context, userId string) ([]*types.Container, error)
	FindByAgentID(ctx context.Context, containerId string) (*types.Container, error)
	FindAll(ctx context.Context) ([]*types.Container, error)

	GetAgentStats(ctx context.Context) ([]types.AgentStats, error)
}

type DockerTemplateImpl struct {
	blobstorage blobstorage.BlobStorage
	client      client.APIClient
}

func NewDockerTemplateImpl(blobstorage blobstorage.BlobStorage) *DockerTemplateImpl {
	return &DockerTemplateImpl{
		blobstorage: blobstorage,
	}
}

func (t *DockerTemplateImpl) Start(ctx context.Context) error {
	client, err := client.New(client.FromEnv)
	if err != nil {
		return fmt.Errorf("error creating docker cdlient: %w", err)
	}

	err = t.blobstorage.Start(ctx)
	if err != nil {
		return fmt.Errorf("error starting blobstorage: %w", err)
	}

	t.client = client

	if err = CreateStateFile(); err != nil {
		return err
	}

	return nil
}

func (t *DockerTemplateImpl) Close() {
	t.client.Close()
}

func (t *DockerTemplateImpl) PullPicoclawImage(ctx context.Context) error {
	image := config.AppConfig.PicoclawImage

	slog.Debug("pulling picoclaw image",
		slog.String("image", image))

	res, err := t.client.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("error pulling picoclaw image: %w", err)
	}

	slog.Debug("picoclaw image pulled",
		slog.String("image", image))

	io.Copy(io.Discard, res)
	res.Close()

	return nil
}

func (t *DockerTemplateImpl) CreateAgentContainer(ctx context.Context, agent *types.Agent) (*types.Container, error) {
	slog.Debug("creating container",
		slog.String("agentID", agent.ID),
		slog.String("userID", agent.UserID),
	)

	res, err := t.FindByAgentID(ctx, agent.ID)
	if err != nil {
		return nil, fmt.Errorf("error checking for duplicate containers: %w", err)
	}

	if res != nil {
		slog.Debug("container already exists",
			slog.String("agentID", agent.ID),
			slog.String("userID", agent.UserID),
		)
		return res, nil
	}

	labels := make(map[string]string)
	labels["userId"] = agent.UserID
	labels["agentId"] = agent.ID

	err = t.blobstorage.LoadWorkspace(ctx, agent)
	if err != nil {
		return nil, fmt.Errorf("error loading remote workspace: %w", err)
	}

	err = os.MkdirAll(agent.GetHostLogsPath(), 0755)
	if err != nil {
		return nil, fmt.Errorf("error creating logs dir: %w", err)
	}

	_, err = os.OpenFile(agent.GetHostConfigPath(), os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("error creating config file: %w", err)
	}

	createOptions := client.ContainerCreateOptions{
		Image: config.AppConfig.PicoclawImage,
		Name:  agent.ID,
		Config: &container.Config{
			Labels: labels,
			User:   agent.Name,
		},
		HostConfig: &container.HostConfig{
			Resources: agent.GetResources(),
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: agent.GetHostWorkspacePath(),
					Target: agent.GetAgentWorkspacePath(),
				},
				{
					Type:     mount.TypeBind,
					Source:   agent.GetHostLogsPath(),
					Target:   agent.GetAgentLogsPath(),
					ReadOnly: true,
				},
				{
					Type:   mount.TypeBind,
					Source: agent.GetHostConfigPath(),
					Target: agent.GetAgentConfigPath(),
				},
			},
		},
	}

	created, err := t.client.ContainerCreate(ctx, createOptions)
	if err != nil {
		return nil, fmt.Errorf("error creating container %s: %w", agent.ID, err)
	}

	c := &types.Container{
		ID:      created.ID,
		AgentID: agent.ID,
		UserID:  agent.UserID,
		State:   "created",
	}

	if err = AddAgentToState(agent, c); err != nil {
		slog.Error("error adding agent to state file",
			slog.String("agentID", agent.ID),
			slog.Any("error", err),
		)
	}

	slog.Debug("container created",
		slog.String("agentID", agent.ID),
		slog.String("userID", agent.UserID),
	)

	return c, nil
}

func (t *DockerTemplateImpl) StartAgentContainer(ctx context.Context, agent *types.Agent, container *types.Container) error {
	slog.Debug("starting container",
		slog.String("agentID", agent.ID),
		slog.String("userID", agent.UserID),
	)

	_, err := t.client.ContainerStart(ctx, container.ID, client.ContainerStartOptions{})
	if err != nil {
		return fmt.Errorf("error starting container: %w", err)
	}

	slog.Debug("container started",
		slog.String("agentID", agent.ID),
		slog.String("userID", agent.UserID),
	)
	return nil
}

func (t *DockerTemplateImpl) FindByUserID(ctx context.Context, userID string) ([]*types.Container, error) {
	filters := make(client.Filters)
	filters.Add("label", "userId="+userID)

	options := client.ContainerListOptions{
		Filters: filters,
		All:     true,
	}

	res, err := t.client.ContainerList(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("error listing all containers: %w", err)
	}

	containers := make([]*types.Container, 0)
	for _, item := range res.Items {
		containers = append(containers, &types.Container{
			ID:      item.ID,
			UserID:  item.Labels["userId"],
			AgentID: item.Labels["agentId"],
			State:   item.State,
		})
	}

	return containers, nil
}

func (t *DockerTemplateImpl) FindByAgentID(ctx context.Context, agentID string) (*types.Container, error) {
	filters := make(client.Filters)
	filters.Add("label", "agentId="+agentID)
	listOptions := client.ContainerListOptions{
		Filters: filters,
		All:     true,
	}

	res, err := t.client.ContainerList(ctx, listOptions)
	if err != nil {
		return nil, fmt.Errorf("error listing containers by agent id: %w", err)
	}

	if len(res.Items) == 0 {
		return nil, nil
	}

	item := res.Items[0]
	return &types.Container{
		ID:      item.ID,
		AgentID: item.Labels["agentId"],
		UserID:  item.Labels["userId"],
		State:   item.State,
	}, nil
}

func (t *DockerTemplateImpl) FindAll(ctx context.Context) ([]*types.Container, error) {
	res, err := t.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("error listing all containers: %w", err)
	}

	containers := make([]*types.Container, 0)
	for _, item := range res.Items {
		containers = append(containers, &types.Container{
			ID:      item.ID,
			AgentID: item.Labels["agentId"],
			UserID:  item.Labels["userId"],
			State:   item.State,
		})
	}

	return containers, nil
}

func (t *DockerTemplateImpl) GetAgentStats(ctx context.Context) ([]types.AgentStats, error) {
	states, err := ReadStateFile()
	if err != nil {
		return nil, fmt.Errorf("failed to read from state file: %w", err)
	}

	agentStats := make([]types.AgentStats, 0)
	for _, state := range states {
		res, err := t.client.ContainerStats(ctx, state.Container.ID, client.ContainerStatsOptions{})
		if err != nil {
			slog.Error("error getting container stats", slog.String("agentID", state.Agent.ID), slog.Any("error", err))
			continue
		}

		var stats container.StatsResponse
		err = json.NewDecoder(res.Body).Decode(&stats)
		res.Body.Close()
		if err != nil {
			slog.Error("error parsing stats response", slog.String("agentID", state.Agent.ID), slog.Any("error", err))
			continue
		}

		agentStats = append(agentStats, types.AgentStats{
			AgentID: state.Agent.ID,
			CpuUsage: types.CpuUsage{
				CpuLimit: state.Agent.Tier.Limits.Cpu,
				CpuUsed:  CalculateCpuUsed(&stats),
			},
			MemoryUsage: types.MemoryUsage{
				MemoryLimitMb: state.Agent.Tier.Limits.MemoryMb,
				MemoryUsedMb:  CalculateMemoryUsed(&stats),
			},
		})
	}

	return agentStats, nil
}
