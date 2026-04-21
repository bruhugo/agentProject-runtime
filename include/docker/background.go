package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/bruhugo/PicoClawProjectRuntime/include/config"
	"github.com/bruhugo/PicoClawProjectRuntime/include/types"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

const BUFFER_SIZE = 5 * 1024 * 1024

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

func (dockerTemplate DockerTemplateImpl) streamVpsMetrics(ctx context.Context) error {
	slog.Info("starting vps metrics streaming")
	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			slog.Debug("collecting vps metrics")
			memStat, err := mem.VirtualMemoryWithContext(ctx)
			if err != nil {
				slog.Error("error getting memory stats", "error", err)
				continue
			}

			cpuPercent, err := cpu.PercentWithContext(ctx, time.Second, false)
			if err != nil {
				slog.Error("error getting cpu percent", "error", err)
				continue
			}

			cpuCount, err := cpu.CountsWithContext(ctx, true)
			if err != nil {
				slog.Error("error getting cpu count", "error", err)
				continue
			}

			vpsStats := types.VpsStats{
				CpuUsage: types.CpuUsage{
					CpuUsed:  (cpuPercent[0] / 100.0) * float64(cpuCount),
					CpuLimit: float64(cpuCount),
				},
				MemoryUsage: types.MemoryUsage{
					MemoryUsedMb:  memStat.Used / (1024 * 1024),
					MemoryLimitMb: memStat.Total / (1024 * 1024),
				},
			}

			stats, err := dockerTemplate.GetAgentStats(ctx)
			if err != nil {
				slog.Error("error getting stats from agent",
					"error", err.Error())
				// Continue anyway, maybe host metrics are still valuable
			}

			vpsStats.Agents = stats

			jsonData, err := json.Marshal(vpsStats)
			if err != nil {
				slog.Error("error marshaling vps stats", "error", err)
				continue
			}

			url := fmt.Sprintf("http://%s:%d/api/v1/vps/metrics", config.AppConfig.MainNodeAddress, config.AppConfig.MainNodePort)
			req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
			if err != nil {
				slog.Error("error creating metrics request", "error", err)
				continue
			}
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				slog.Error("error sending vps metrics", "error", err)
				continue
			}
			resp.Body.Close()

			if resp.StatusCode >= 400 {
				slog.Error("received error response from main node", "status", resp.Status)
			} else {
				slog.Debug("vps metrics sent successfully")
			}

		case <-ctx.Done():
			slog.Info("stopping vps metrics streaming")
			return nil
		}
	}
}

func (t *DockerTemplateImpl) syncWorkspace(agent *types.Agent) {
	slog.Info("starting workspace sync background task", "agentId", agent.ID)
	ctx, ok := t.agentContext[agent.ID]
	if !ok {
		slog.Error("must define the agent context before sending logs",
			"agentId", agent.ID,
			"userId", agent.UserID)
		return
	}

	ticker := time.NewTicker(time.Second * 60)
	for {
		select {
		case <-ticker.C:
			slog.Debug("triggering workspace sync", "agentId", agent.ID)
			t.blobstorage.SyncWorkspace(ctx.ctx, agent)
		case <-ctx.ctx.Done():
			slog.Info("stopping workspace sync background task", "agentId", agent.ID)
			return
		}
	}
}

func (t *DockerTemplateImpl) sendLogs(agent *types.Agent, container *types.Container) {
	slog.Info("starting log streaming background task", "agentId", agent.ID)
	ctx, ok := t.agentContext[agent.ID]
	if !ok {
		slog.Error("must define the agent context before sending logs",
			"agentId", agent.ID,
			"userId", agent.UserID)
		return
	}

	buffer := bytes.NewBuffer(make([]byte, BUFFER_SIZE))
	logs, err := t.client.ContainerLogs(ctx.ctx, container.ID, client.ContainerLogsOptions{})
	if err != nil {
		slog.Error("error opening logs", "agent", agent.ID, "user", agent.UserID)
		return
	}
	scanner := bufio.NewScanner(logs)

	var mutex sync.Mutex
	ticker := time.NewTicker(4 * time.Minute)

	go func() {
		for {
			select {
			case <-ticker.C:
				mutex.Lock()
				defer mutex.Unlock()

				// empty logs
				if len(buffer.Bytes()) == 0 {
					continue
				}

				remotepath := filepath.Join(types.GetRemoteLogsPath(agent.ID), time.Now().Local().String())
				slog.Debug("uploading logs due to timeout", "agentId", agent.ID, "remotePath", remotepath)
				err = t.blobstorage.UploadBuffer(ctx.ctx, buffer, remotepath)
				if err != nil {
					slog.Error("error sending log file",
						"agentId", agent.ID,
						"userId", agent.UserID,
						"error", err.Error())
					continue
				}
				buffer.Reset()
			case <-ctx.ctx.Done():
				slog.Info("stopping log streaming background task (timer)", "agentId", agent.ID)
				return
			}
		}
	}()

	for scanner.Scan() {
		mutex.Lock()
		defer mutex.Unlock()

		if len(scanner.Bytes())+buffer.Len() >= BUFFER_SIZE {
			remotepath := filepath.Join(types.GetRemoteLogsPath(agent.ID), time.Now().Local().String())
			slog.Debug("uploading logs due to buffer full", "agentId", agent.ID, "remotePath", remotepath)
			err = t.blobstorage.UploadBuffer(ctx.ctx, buffer, remotepath)
			if err != nil {
				slog.Error("error sending log file",
					"agentId", agent.ID,
					"userId", agent.UserID,
					"error", err.Error())
				continue
			}
			slog.Info("logs uploaded successfully",
				"agentId", agent.ID,
				"userId", agent.UserID)
			buffer.Reset()
		}

		buffer.Write(scanner.Bytes())
		buffer.WriteByte('\n')
	}
	slog.Info("stopping log streaming background task (scanner finished)", "agentId", agent.ID)
}
