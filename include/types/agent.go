package types

import (
	"fmt"

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

func (agent Agent) GetHostWorkspacePath() string {
	return fmt.Sprintf("/home/%s/agents/%s/workspace", config.AppConfig.SystemUser, agent.ID)
}
func (agent Agent) GetAgentWorkspacePath() string {
	return fmt.Sprintf("/home/%s/agents/.picoclaw/workspace", agent.Name)
}

func (agent Agent) GetHostLogsPath() string {
	return fmt.Sprintf("/home/%s/agents/%s/logs", config.AppConfig.SystemUser, agent.ID)
}
func (agent Agent) GetAgentLogsPath() string {
	return fmt.Sprintf("/home/%s/agents/.picoclaw/logs", agent.Name)
}

func (agent Agent) GetHostConfigPath() string {
	return fmt.Sprintf("/home/%s/agents/%s/config.json", config.AppConfig.SystemUser, agent.ID)
}
func (agent Agent) GetAgentConfigPath() string {
	return fmt.Sprintf("/home/%s/agents/.picoclaw/config.json", agent.Name)
}
