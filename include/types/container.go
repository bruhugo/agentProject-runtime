package types

import "github.com/moby/moby/api/types/container"

type Container struct {
	ID      string                   `json:"id"`
	AgentID string                   `json:"agent_id"`
	UserID  string                   `json:"user_id"`
	State   container.ContainerState `json:"state"`
}
