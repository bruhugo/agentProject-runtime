package types

type Vps struct {
	IPv4              string  `json:"ipv4"`
	IPv6              string  `json:"ipv6"`
	MemoryMb          int32   `json:"memory_mb"`
	CPU               int32   `json:"cpu"`
	AvailableMemoryMb int32   `json:"available_memory_mb"`
	AvailableCPU      int32   `json:"available_cpu"`
	AgentNumber       int32   `json:"agent_number"`
	Agents            []Agent `json:"agents"`
}
