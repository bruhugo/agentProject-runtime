package types

type CpuUsage struct {
	CpuUsed  float64 `json:"cpu_used"`
	CpuLimit float64 `json:"cpu_available"`
}

type MemoryUsage struct {
	MemoryUsedMb  uint64 `json:"memory_used_mb"`
	MemoryLimitMb uint64 `json:"memory_available_mb"`
}

type AgentStats struct {
	AgentID     string
	MemoryUsage MemoryUsage `json:"memory_usage"`
	CpuUsage    CpuUsage    `json:"cpu_usage"`
}

type VpsStats struct {
	MemoryUsage MemoryUsage  `json:"memory_usage"`
	CpuUsage    CpuUsage     `json:"cpu_usage"`
	Agents      []AgentStats `json:"agents"`
}
