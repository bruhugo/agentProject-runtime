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

func GetAgentResources(agent *Agent) container.Resources {
	return container.Resources{
		NanoCPUs: int64(agent.Tier.Limits.Cpu * 1e9),

		Memory:     int64(agent.Tier.Limits.MemoryMb * 1024 * 1024),
		MemorySwap: int64(agent.Tier.Limits.MemoryMb * 1024 * 1024),
	}
}

// HOST PATH 	-> the path on the host machine
// AGENT PATH 	-> the path inside the agent's container
// REMOTE PATH	-> blobstorage key

func GetHostWorkspacePath(agentID string) string {
	return filepath.Join("/home", config.AppConfig.SystemUser, "agents", agentID, "workspace")
}
func GetHostConfigPath(agentID string) string {
	return filepath.Join("/home", config.AppConfig.SystemUser, "agents", agentID, "config.json")
}
func GetHostLogsPath(agentID string) string {
	return filepath.Join("/home", config.AppConfig.SystemUser, "agents", agentID, "logs")
}

func GetAgentLogsPath() string {
	return filepath.Join("/root", ".picoclaw", "logs")
}
func GetAgentWorkspacePath() string {
	return filepath.Join("/root", ".picoclaw", "workspace")
}
func GetAgentConfigPath() string {
	return filepath.Join("/root", ".picoclaw", "config.json")
}

func GetRemoteWorkspacePath(agentID string) string {
	return filepath.Join("agents", agentID, "workspace")
}
func GetRemoteLogsPath(agentID string) string {
	return filepath.Join("agents", agentID, "logs")
}
func GetRemoteConfigPath(agentID string) string {
	return filepath.Join("agents", agentID, "config.json")
}
