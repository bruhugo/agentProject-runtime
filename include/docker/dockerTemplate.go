package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/bruhugo/PicoClawProjectRuntime/include/blobstorage"
	"github.com/bruhugo/PicoClawProjectRuntime/include/config"
	"github.com/bruhugo/PicoClawProjectRuntime/include/types"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

type Container struct {
	ID      string                   `json:"id"`
	AgentID string                   `json:"agent_id"`
	UserID  string                   `json:"user_id"`
	State   container.ContainerState `json:"state"`
}

type DockerTemplate interface {
	Start(ctx context.Context) error
	Close()

	PullPicoclawImage(ctx context.Context) error

	CreateAgentContainer(ctx context.Context, agent *types.Agent) (*Container, error)
	StartAgentContainer(ctx context.Context, agent *types.Agent, container *Container) error

	FindByUserID(ctx context.Context, userId string) ([]*Container, error)
	FindByAgentID(ctx context.Context, containerId string) (*Container, error)
	FindAll(ctx context.Context) ([]*Container, error)

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

func (t *DockerTemplateImpl) CreateAgentContainer(ctx context.Context, agent *types.Agent) (*Container, error) {
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

	t.blobstorage.LoadWorkspace(ctx, agent)

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

	slog.Debug("container created",
		slog.String("agentID", agent.ID),
		slog.String("userID", agent.UserID),
	)

	return &Container{
		ID:      created.ID,
		AgentID: agent.ID,
		UserID:  agent.UserID,
		State:   "created",
	}, nil
}

func (t *DockerTemplateImpl) StartAgentContainer(ctx context.Context, agent *types.Agent, container *Container) error {
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

func (t *DockerTemplateImpl) FindByUserID(ctx context.Context, userID string) ([]*Container, error) {
	filters := make(client.Filters)
	filters.Add("label", "userId="+userID)

	options := client.ContainerListOptions{
		Filters: filters,
	}

	res, err := t.client.ContainerList(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("error listing all containers: %w", err)
	}

	containers := make([]*Container, 0)
	for _, item := range res.Items {
		containers = append(containers, &Container{
			ID:      item.ID,
			UserID:  item.Labels["userId"],
			AgentID: item.Labels["name"],
			State:   item.State,
		})
	}

	return containers, nil
}

func (t *DockerTemplateImpl) FindByAgentID(ctx context.Context, agentID string) (*Container, error) {
	filters := make(client.Filters)
	filters.Add("name", agentID)
	listOptions := client.ContainerListOptions{
		Filters: filters,
		All:     false,
	}

	res, err := t.client.ContainerList(ctx, listOptions)
	if err != nil {
		return nil, fmt.Errorf("error listing containers by agent id: %w", err)
	}

	if len(res.Items) == 0 {
		return nil, nil
	}

	container := &Container{
		ID:      res.Items[0].ID,
		AgentID: agentID,
		UserID:  res.Items[0].Labels["userId"],
		State:   res.Items[0].State,
	}

	return container, nil
}

func (t *DockerTemplateImpl) FindAll(ctx context.Context) ([]*Container, error) {
	res, err := t.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("error listing all containers: %w", err)
	}

	containers := make([]*Container, 0)
	for _, item := range res.Items {
		containers = append(containers, &Container{
			ID:      item.ID,
			AgentID: item.Labels["name"],
			UserID:  item.Labels["userId"],
			State:   item.State,
		})
	}

	return containers, nil
}

func (t *DockerTemplateImpl) GetAgentStats(ctx context.Context) ([]types.AgentStats, error) {
	return nil, nil
}
