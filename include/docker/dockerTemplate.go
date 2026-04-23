package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

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

	DeleteAgent(ctx context.Context, agentID string) error

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

	agentContext   map[string]AgentCtx
	agentContextMu sync.RWMutex
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
	c, err := client.New(client.FromEnv)
	if err != nil {
		return fmt.Errorf("error creating docker cdlient: %w", err)
	}

	err = t.blobstorage.Start(t.appCtx)
	if err != nil {
		return fmt.Errorf("error starting blobstorage: %w", err)
	}

	t.client = c

	if err = t.PullPicoclawImage(t.appCtx); err != nil {
		return fmt.Errorf("error pulling picoclaw image: %s", err)
	}

	// Recovering background tasks for existing containers
	filters := make(client.Filters)
	filters.Add("label", "agentId")
	res, err := t.client.ContainerList(t.appCtx, client.ContainerListOptions{Filters: filters, All: true})
	if err == nil {
		for _, c := range res.Items {
			agentID := c.Labels["agentId"]
			userID := c.Labels["userId"]
			agent := &types.Agent{ID: agentID, UserID: userID}
			containerObj := &types.Container{ID: c.ID, AgentID: agentID, UserID: userID}

			// If container is not running, try to start it
			if c.State != "running" {
				slog.Info("restarting existing container", "agentId", agentID, "state", c.State)
				if _, err := t.client.ContainerStart(t.appCtx, c.ID, client.ContainerStartOptions{}); err != nil {
					slog.Error("failed to restart existing container", "agentId", agentID, "error", err)
					continue
				}
			}

			agentContext, cancel := context.WithCancel(t.appCtx)
			t.agentContextMu.Lock()
			t.agentContext[agentID] = AgentCtx{ctx: agentContext, cancel: cancel}
			t.agentContextMu.Unlock()

			go t.sendLogs(agent, containerObj)
			go t.syncWorkspace(agent)
		}
	}

	go t.streamVpsMetrics(t.appCtx)

	return nil
}

func (t *DockerTemplateImpl) Close() {
	slog.Info("closing docker template")
	t.client.Close()
	t.agentContextMu.Lock()
	defer t.agentContextMu.Unlock()
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
	labels["cpuLimit"] = fmt.Sprintf("%f", agent.Tier.Limits.Cpu)
	labels["memLimit"] = fmt.Sprintf("%d", agent.Tier.Limits.MemoryMb)

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

	slog.Info("container started successfully",
		slog.String("agentID", agent.ID),
		slog.String("userID", agent.UserID),
	)

	agentContext, cancel := context.WithCancel(t.appCtx)
	t.agentContextMu.Lock()
	t.agentContext[agent.ID] = AgentCtx{
		ctx:    agentContext,
		cancel: cancel,
	}
	t.agentContextMu.Unlock()

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
	filters := make(client.Filters)
	filters.Add("label", "agentId")

	res, err := t.client.ContainerList(ctx, client.ContainerListOptions{
		Filters: filters,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	agentStats := make([]types.AgentStats, 0)
	for _, c := range res.Items {
		agentID := c.Labels["agentId"]

		res, err := t.client.ContainerStats(ctx, c.ID, client.ContainerStatsOptions{Stream: false})
		if err != nil {
			slog.Error("error getting container stats", slog.String("agentID", agentID), slog.Any("error", err))
			continue
		}

		var stats container.StatsResponse
		err = json.NewDecoder(res.Body).Decode(&stats)
		res.Body.Close()
		if err != nil {
			slog.Error("error parsing stats response", slog.String("agentID", agentID), slog.Any("error", err))
			continue
		}

		var cpuLimit float64
		var memLimit uint64
		fmt.Sscanf(c.Labels["cpuLimit"], "%f", &cpuLimit)
		fmt.Sscanf(c.Labels["memLimit"], "%d", &memLimit)

		agentStats = append(agentStats, types.AgentStats{
			AgentID: agentID,
			CpuUsage: types.CpuUsage{
				CpuLimit: cpuLimit,
				CpuUsed:  CalculateCpuUsed(&stats),
			},
			MemoryUsage: types.MemoryUsage{
				MemoryLimitMb: memLimit,
				MemoryUsedMb:  CalculateMemoryUsed(&stats),
			},
		})
	}

	return agentStats, nil
}

func (t *DockerTemplateImpl) GetSingleAgentStats(ctx context.Context, agentID string) (*types.AgentStats, error) {
	c, err := t.FindByAgentID(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving container: %w", err)
	}

	res, err := t.client.ContainerStats(ctx, c.ID, client.ContainerStatsOptions{Stream: false})
	if err != nil {
		return nil, fmt.Errorf("error retrieving container stats: %w", err)
	}

	var stats container.StatsResponse
	err = json.NewDecoder(res.Body).Decode(&stats)
	res.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("error parsing response json: %w", err)
	}

	inspect, err := t.client.ContainerInspect(ctx, c.ID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("error inspecting container: %w", err)
	}

	var cpuLimit float64
	var memLimit uint64
	fmt.Sscanf(inspect.Container.Config.Labels["cpuLimit"], "%f", &cpuLimit)
	fmt.Sscanf(inspect.Container.Config.Labels["memLimit"], "%d", &memLimit)

	return &types.AgentStats{
		AgentID: c.AgentID,
		CpuUsage: types.CpuUsage{
			CpuLimit: cpuLimit,
			CpuUsed:  CalculateCpuUsed(&stats),
		},
		MemoryUsage: types.MemoryUsage{
			MemoryLimitMb: memLimit,
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

	slog.Info("agent stopped successfully", "agentId", agentId)
	return nil
}

func (t *DockerTemplateImpl) DeleteAgent(ctx context.Context, agentID string) error {
	slog.Info("deleting agent", "agentId", agentID)

	err := t.StopAgent(ctx, agentID)
	if err != nil {
		slog.Warn("error stopping agent during deletion, proceeding anyway", "agentId", agentID, "error", err)
	}

	t.agentContextMu.Lock()
	if agentCtx, ok := t.agentContext[agentID]; ok {
		agentCtx.cancel()
		delete(t.agentContext, agentID)
	}
	t.agentContextMu.Unlock()

	slog.Debug("performing final workspace sync", "agentId", agentID)
	agent := &types.Agent{ID: agentID} // We only need ID for paths
	err = t.blobstorage.SyncWorkspace(ctx, agent)
	if err != nil {
		slog.Error("failed to perform final workspace sync", "agentId", agentID, "error", err)
	}

	c, err := t.FindByAgentID(ctx, agentID)
	if err != nil {
		slog.Error("error finding container for removal", "agentId", agentID, "error", err)
	} else if c != nil {
		_, err = t.client.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true})
		if err != nil {
			return fmt.Errorf("error removing container: %w", err)
		}
	}

	hostAgentDir := filepath.Dir(types.GetHostWorkspacePath(agentID))
	slog.Debug("cleaning up local host directory", "path", hostAgentDir)
	err = os.RemoveAll(hostAgentDir)
	if err != nil {
		slog.Error("failed to remove local agent directory", "path", hostAgentDir, "error", err)
	}

	slog.Info("agent deleted successfully", "agentId", agentID)
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
