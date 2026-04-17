package docker

import "github.com/moby/moby/api/types/container"

func CalculateCpuUsed(stats *container.StatsResponse) float64 {
	cpuDelta := stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage
	systemDelta := stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage
	onlineCPUs := float64(stats.CPUStats.OnlineCPUs)

	if onlineCPUs == 0 {
		onlineCPUs = 1.0
	}

	if systemDelta > 0.0 && cpuDelta > 0.0 {
		return (float64(cpuDelta) / float64(systemDelta)) * onlineCPUs
	}
	return 0
}

func CalculateMemoryUsed(stats *container.StatsResponse) uint64 {
	return stats.MemoryStats.Usage / (1024 * 1024)
}
