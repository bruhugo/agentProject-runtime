package types

import (
	"path/filepath"

	"github.com/bruhugo/PicoClawProjectRuntime/include/config"
	"github.com/moby/moby/api/types/container"
)

type State string

const (
	CREATED   State = "created"
	STARTING  State = "starting"
	RUNNING   State = "running"
	STOPPED   State = "stopped"
	DESTROYED State = "destroyed"
)

type Agent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	UserID      string `json:"user_id"`
	Description string `json:"description"`
	State       State  `json:"state"`
	Tier        Tier   `json:"tier"`
}

func (agent Agent) GetResources() container.Resources {
	return container.Resources{
		NanoCPUs: int64(agent.Tier.Limits.Cpu * 1e9),

		Memory:     int64(agent.Tier.Limits.MemoryMb * 1024 * 1024),
		MemorySwap: int64(agent.Tier.Limits.MemoryMb * 1024 * 1024),
	}
}

// HOST PATH 	-> the path on the host machine
// AGENT PATH 	-> the path inside the agent's container
// REMOTE PATH	-> blobstorage key

func (agent Agent) GetHostWorkspacePath() string {
	return filepath.Join("/home", config.AppConfig.SystemUser, "agents", agent.ID, "workspace")
}
func (agent Agent) GetHostConfigPath() string {
	return filepath.Join("/home", config.AppConfig.SystemUser, "agents", agent.ID, "config.json")
}
func (agent Agent) GetHostLogsPath() string {
	return filepath.Join("/home", config.AppConfig.SystemUser, "agents", agent.ID, "logs")
}

func (agent Agent) GetAgentLogsPath() string {
	return filepath.Join("/root", ".picoclaw", "logs")
}
func (agent Agent) GetAgentWorkspacePath() string {
	return filepath.Join("/root", ".picoclaw", "workspace")
}
func (agent Agent) GetAgentConfigPath() string {
	return filepath.Join("/root", ".picoclaw", "config.json")
}

func (agent Agent) GetRemoteWorkspacePath() string {
	return filepath.Join("agents", agent.ID, "workspace")
}
func (agent Agent) GetRemoteLogsPath() string {
	return filepath.Join("agents", agent.ID, "logs")
}
func (agent Agent) GetRemoteConfigPath() string {
	return filepath.Join("agents", agent.ID, "config.json")
}
