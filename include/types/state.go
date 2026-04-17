package types

type AgentState struct {
	Agent      Agent     `json:"agent"`
	Container  Container `json:"container"`
	Retries    uint32    `json:"retries"`
	MaxRetries uint32    `json:"max_retries"`
}
