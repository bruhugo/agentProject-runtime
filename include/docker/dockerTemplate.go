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
	Start() error
	Close()

	PullPicoclawImage(ctx context.Context) error

	CreateAgentContainer(ctx context.Context, agent *types.Agent) (*types.Container, error)
	StartAgentContainer(ctx context.Context, agent *types.Agent, container *types.Container) error
	StopAgent(ctx context.Context, agentId string) error

	FindByUserID(ctx context.Context, userId string) ([]*types.Container, error)
	FindByAgentID(ctx context.Context, containerId string) (*types.Container, error)
	FindAll(ctx context.Context) ([]*types.Container, error)

	GetAgentStats(ctx context.Context) ([]types.AgentStats, error)
	GetSingleAgentStats(ctx context.Context, agentID string) (*types.AgentStats, error)

	UpdadeConfigFile(ctx context.Context, agentID string) error
}

type AgentCtx struct {
	ctx    context.Context
	cancel context.CancelFunc
}

type DockerTemplateImpl struct {
	blobstorage blobstorage.BlobStorage
	client      client.APIClient

	appCtx context.Context

	agentContext map[string]AgentCtx
}

func NewDockerTemplateImpl(blobstorage blobstorage.BlobStorage, appCtx context.Context) *DockerTemplateImpl {
	return &DockerTemplateImpl{
		blobstorage:  blobstorage,
		appCtx:       appCtx,
		agentContext: make(map[string]AgentCtx),
	}
}

func (t *DockerTemplateImpl) Start() error {
	slog.Info("starting docker template")
	client, err := client.New(client.FromEnv)
	if err != nil {
		return fmt.Errorf("error creating docker cdlient: %w", err)
	}

	err = t.blobstorage.Start(t.appCtx)
	if err != nil {
		return fmt.Errorf("error starting blobstorage: %w", err)
	}

	t.client = client

	if err = t.PullPicoclawImage(t.appCtx); err != nil {
		return fmt.Errorf("error pulling picoclaw image: %s", err)
	}

	if err = CreateStateFile(); err != nil {
		return err
	}
	previousStates, err := ReadStateFile()
	if err != nil {
		return err
	}

	if len(previousStates) > 0 {
		slog.Info("restarting previous containers", "count", len(previousStates))
	}

	for _, state := range previousStates {
		if t.StartAgentContainer(t.appCtx, &state.Agent, &state.Container) != nil {
			return fmt.Errorf("error starting previous containers")
		}
	}

	go t.streamVpsMetrics(t.appCtx)

	return nil
}

func (t *DockerTemplateImpl) Close() {
	slog.Info("closing docker template")
	t.client.Close()
	for id, agentContext := range t.agentContext {
		slog.Debug("cancelling agent context", "agentId", id)
		agentContext.cancel()
	}
}

func (t *DockerTemplateImpl) PullPicoclawImage(ctx context.Context) error {
	image := config.AppConfig.PicoclawImage

	slog.Info("pulling picoclaw image",
		slog.String("image", image))

	res, err := t.client.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("error pulling picoclaw image: %w", err)
	}

	slog.Info("picoclaw image pulled",
		slog.String("image", image))

	io.Copy(io.Discard, res)
	res.Close()

	return nil
}

func (t *DockerTemplateImpl) CreateAgentContainer(ctx context.Context, agent *types.Agent) (*types.Container, error) {
	slog.Info("creating container",
		slog.String("agentID", agent.ID),
		slog.String("userID", agent.UserID),
	)

	res, err := t.FindByAgentID(ctx, agent.ID)
	if err != nil {
		return nil, fmt.Errorf("error checking for duplicate containers: %w", err)
	}

	if res != nil {
		slog.Info("container already exists, returning existing one",
			slog.String("agentID", agent.ID),
			slog.String("userID", agent.UserID),
		)
		return res, nil
	}

	labels := make(map[string]string)
	labels["userId"] = agent.UserID
	labels["agentId"] = agent.ID

	slog.Debug("loading workspace from blobstorage", "agentId", agent.ID)
	err = t.blobstorage.LoadWorkspace(ctx, agent)
	if err != nil {
		return nil, fmt.Errorf("error loading remote workspace: %w", err)
	}

	slog.Debug("loading config from blobstorage", "agentId", agent.ID)
	err = t.blobstorage.GetFile(ctx, types.GetHostConfigPath(agent.ID), types.GetRemoteConfigPath(agent.ID))
	if err != nil {
		return nil, fmt.Errorf("error loading remote config.json: %w", err)
	}

	err = os.MkdirAll(types.GetHostLogsPath(agent.ID), 0755)
	if err != nil {
		return nil, fmt.Errorf("error creating logs dir: %w", err)
	}

	createOptions := client.ContainerCreateOptions{
		Image: config.AppConfig.PicoclawImage,
		Name:  agent.ID,
		Config: &container.Config{
			Labels: labels,
		},
		HostConfig: &container.HostConfig{
			Resources: types.GetAgentResources(agent),
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: types.GetHostWorkspacePath(agent.ID),
					Target: types.GetAgentWorkspacePath(),
				},
				{
					Type:   mount.TypeBind,
					Source: types.GetHostLogsPath(agent.ID),
					Target: types.GetAgentLogsPath(),
				},
				{
					Type:     mount.TypeBind,
					Source:   types.GetHostConfigPath(agent.ID),
					Target:   types.GetAgentConfigPath(),
					ReadOnly: true,
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

	slog.Info("container created successfully",
		slog.String("agentID", agent.ID),
		slog.String("userID", agent.UserID),
		slog.String("containerID", c.ID),
	)

	return c, nil
}

func (t *DockerTemplateImpl) StartAgentContainer(ctx context.Context, agent *types.Agent, container *types.Container) error {
	slog.Info("starting container",
		slog.String("agentID", agent.ID),
		slog.String("userID", agent.UserID),
	)

	_, err := t.client.ContainerStart(ctx, container.ID, client.ContainerStartOptions{})
	if err != nil {
		return fmt.Errorf("error starting container: %w", err)
	}

	if err = AddAgentToState(agent, container); err != nil {
		slog.Error("error adding agent to state file",
			slog.String("agentID", agent.ID),
			slog.Any("error", err),
		)
	}

	slog.Info("container started successfully",
		slog.String("agentID", agent.ID),
		slog.String("userID", agent.UserID),
	)

	agentContext, cancel := context.WithCancel(t.appCtx)
	t.agentContext[agent.ID] = AgentCtx{
		ctx:    agentContext,
		cancel: cancel,
	}

	slog.Debug("starting background tasks for agent", "agentId", agent.ID)
	go t.sendLogs(agent, container)
	go t.syncWorkspace(agent)

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

func (t *DockerTemplateImpl) GetSingleAgentStats(ctx context.Context, agentID string) (*types.AgentStats, error) {
	state, err := FindAgentStateFile(agentID)
	if err != nil {
		return nil, fmt.Errorf("error finding state: %w", err)
	}

	c, err := t.FindByAgentID(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving container: %w", err)
	}

	res, err := t.client.ContainerStats(ctx, c.ID, client.ContainerStatsOptions{})
	if err != nil {
		return nil, fmt.Errorf("error retrieving container stats: %w", err)
	}

	var stats container.StatsResponse
	err = json.NewDecoder(res.Body).Decode(&stats)
	res.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("error parsing response json: %w", err)
	}
	return &types.AgentStats{
		AgentID: c.AgentID,
		CpuUsage: types.CpuUsage{
			CpuLimit: state.Agent.Tier.Limits.Cpu,
			CpuUsed:  CalculateCpuUsed(&stats),
		},
		MemoryUsage: types.MemoryUsage{
			MemoryLimitMb: state.Agent.Tier.Limits.MemoryMb,
			MemoryUsedMb:  CalculateMemoryUsed(&stats),
		},
	}, nil
}

func (t *DockerTemplateImpl) StopAgent(ctx context.Context, agentId string) error {
	slog.Info("stopping agent", "agentId", agentId)
	c, err := t.FindByAgentID(ctx, agentId)
	if err != nil {
		return fmt.Errorf("error finding container: %w", err)
	}

	// container is already stopped
	if c.State != container.StateRunning {
		slog.Debug("container already stopped", "agentId", agentId, "state", c.State)
		return nil
	}

	_, err = t.client.ContainerStop(ctx, c.ID, client.ContainerStopOptions{})
	if err != nil {
		return fmt.Errorf("error stoping container %s: %w", c.ID, err)
	}

	if err = RemoveAgentFromState(agentId); err != nil {
		return fmt.Errorf("error removing container %s from state: %w", c.ID, err)
	}

	slog.Info("agent stopped successfully", "agentId", agentId)
	return nil
}

func (t *DockerTemplateImpl) UpdadeConfigFile(ctx context.Context, agentID string) error {
	slog.Info("updating config file for agent", "agentId", agentID)
	c, err := t.FindByAgentID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("error finding container: %w", err)
	}

	err = t.blobstorage.GetFile(ctx, types.GetHostConfigPath(agentID), types.GetRemoteConfigPath(agentID))
	if err != nil {
		return fmt.Errorf("error loading config file: %w", err)
	}

	slog.Debug("restarting container after config update", "agentId", agentID)
	_, err = t.client.ContainerRestart(ctx, c.ID, client.ContainerRestartOptions{})
	if err != nil {
		return fmt.Errorf("error restarting container: %w", err)
	}

	slog.Info("agent config updated and container restarted", "agentId", agentID)
	return nil
}
