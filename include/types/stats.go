package types

type AgentStats struct {
	ID           string
	UserID       string
	State        State
	MemoryMb     int
	Cpu          int
	UsedMemoryMb int
	UsedCpu      int
}

type VpsStats struct {
	MemoryMb     int
	Cpu          int
	UsedMemoryMb int
	UsedCpu      int
	AgentStatus  []AgentStats
}
