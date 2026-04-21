package handlers

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/bruhugo/PicoClawProjectRuntime/include/docker"
	"github.com/bruhugo/PicoClawProjectRuntime/include/errors"
	"github.com/bruhugo/PicoClawProjectRuntime/include/types"
	"github.com/gin-gonic/gin"
)

type AgentHandler struct {
	dockerTemplate docker.DockerTemplate
}

func getAgentId(ctx *gin.Context) (string, error) {
	agendId := ctx.Param("agentId")
	if agendId == "" {
		return "", &errors.HttpAgentError{
			Message: "no agent id provided",
			Err:     fmt.Errorf("no agent id provided"),
		}
	}
	return agendId, nil
}

func NewAgentHanlder(dockerTemplate docker.DockerTemplate) *AgentHandler {
	return &AgentHandler{
		dockerTemplate: dockerTemplate,
	}
}

func (h *AgentHandler) CreateAgent(ctx *gin.Context) {
	var agent types.Agent
	err := ctx.ShouldBindBodyWithJSON(&agent)
	if err != nil {
		ctx.Error(errors.NewBadRequestError("Error mapping body to json", err, &agent))
		return
	}

	slog.Info("received request to create agent",
		"agentId", agent.ID,
		"userId", agent.UserID)

	container, err := h.dockerTemplate.CreateAgentContainer(ctx, &agent)
	if err != nil {
		ctx.Error(errors.NewServerSideError("Failed to create container", err, &agent))
		return
	}

	err = h.dockerTemplate.StartAgentContainer(ctx, &agent, container)
	if err != nil {
		ctx.Error(errors.NewServerSideError("Failed to start container", err, &agent))
		return
	}

	ctx.JSON(http.StatusCreated, container)
}

func (h *AgentHandler) StopAgent(ctx *gin.Context) {
	agentID, err := getAgentId(ctx)
	if err != nil {
		ctx.Error(err)
		return
	}

	slog.Info("received request to stop agent", "agentId", agentID)

	err = h.dockerTemplate.StopAgent(ctx, agentID)
	if err != nil {
		ctx.Error(&errors.HttpAgentError{
			Message: "Error stopping container",
			Err:     err,
			AgentID: agentID,
		})
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (h *AgentHandler) GetAgentStats(ctx *gin.Context) {
	agentID, err := getAgentId(ctx)
	if err != nil {
		ctx.Error(err)
		return
	}

	slog.Debug("received request for agent stats", "agentId", agentID)

	stats, err := h.dockerTemplate.GetSingleAgentStats(ctx, agentID)
	if err != nil {
		ctx.Error(&errors.HttpAgentError{
			Message: "Error getting stat from agent",
			AgentID: agentID,
			Err:     err,
		})
		return
	}

	ctx.JSON(200, stats)
}

func (h *AgentHandler) UpdadeConfigFile(ctx *gin.Context) {
	agentID, err := getAgentId(ctx)
	if err != nil {
		ctx.Error(err)
		return
	}

	slog.Info("received request to update config file", "agentId", agentID)

	err = h.dockerTemplate.UpdadeConfigFile(ctx, agentID)
	if err != nil {
		ctx.Error(fmt.Errorf("error updating config file: %w", err))
		return
	}

	ctx.Status(http.StatusNoContent)
}
