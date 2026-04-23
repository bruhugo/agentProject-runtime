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
				// just GO :)
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
	t.agentContextMu.RLock()
	ctx, ok := t.agentContext[agent.ID]
	t.agentContextMu.RUnlock()
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
	t.agentContextMu.RLock()
	ctx, ok := t.agentContext[agent.ID]
	t.agentContextMu.RUnlock()
	if !ok {
		slog.Error("must define the agent context before sending logs",
			"agentId", agent.ID,
			"userId", agent.UserID)
		return
	}

	logs, err := t.client.ContainerLogs(ctx.ctx, container.ID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		slog.Error("error opening logs", "agent", agent.ID, "user", agent.UserID, "error", err.Error())
		return
	}
	scanner := bufio.NewScanner(logs)

	var mutex sync.Mutex
	ticker := time.NewTicker(60 * time.Second)

	buffer := bytes.NewBuffer(make([]byte, 0))
	bufferRead := 0
	go func() {
		for {
			select {
			case <-ticker.C:
				mutex.Lock()
				if len(buffer.Bytes()) == 0 {
					mutex.Unlock()
					continue
				}

				remotepath := filepath.Join(types.GetRemoteLogsPath(agent.ID), time.Now().Local().String()+".log")
				slog.Debug("uploading logs due to timeout", "agentId", agent.ID, "remotePath", remotepath)
				err = t.blobstorage.UploadBuffer(ctx.ctx, buffer, remotepath)
				if err != nil {
					slog.Error("error sending log file",
						"agentId", agent.ID,
						"userId", agent.UserID,
						"error", err.Error())
					mutex.Unlock()
					continue
				}
				buffer.Reset()
				bufferRead = 0
				mutex.Unlock()
			case <-ctx.ctx.Done():
				slog.Info("stopping log streaming background task (timer)", "agentId", agent.ID)
				return
			}
		}
	}()

	for scanner.Scan() {
		mutex.Lock()

		if bufferRead+len(scanner.Bytes()) >= BUFFER_SIZE {
			remotepath := filepath.Join(types.GetRemoteLogsPath(agent.ID), time.Now().Local().String()+".log")
			slog.Debug("uploading logs due to buffer full", "agentId", agent.ID, "remotePath", remotepath)
			err = t.blobstorage.UploadBuffer(ctx.ctx, buffer, remotepath)
			if err != nil {
				slog.Error("error sending log file",
					"agentId", agent.ID,
					"userId", agent.UserID,
					"error", err.Error())
				mutex.Unlock()
				continue
			}
			slog.Info("logs uploaded successfully",
				"agentId", agent.ID,
				"userId", agent.UserID)
			buffer.Reset()
			bufferRead = 0
		}

		written, err := buffer.Write(scanner.Bytes())
		if err != nil {
			slog.Error("error writing to log buffer", "agentId", agent.ID, "userId", agent.UserID, "error", err.Error())
			continue
		}

		err = buffer.WriteByte('\n')
		if err != nil {
			slog.Error("error writing to log buffer", "agentId", agent.ID, "userId", agent.UserID, "error", err.Error())
			continue
		}

		bufferRead += written + 1
		mutex.Unlock()
	}
	slog.Info("stopping log streaming background task (scanner finished)", "agentId", agent.ID)
}
