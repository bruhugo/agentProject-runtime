package handlers

import (
	"github.com/bruhugo/PicoClawProjectRuntime/include/docker"
	"github.com/bruhugo/PicoClawProjectRuntime/include/errors"
	"github.com/bruhugo/PicoClawProjectRuntime/include/types"
	"github.com/gin-gonic/gin"
)

type ContainerHandlder struct {
	dockerTemplate docker.DockerTemplate
}

func NewContainerHanlder(dockerTemplate docker.DockerTemplate) *ContainerHandlder {
	return &ContainerHandlder{
		dockerTemplate: dockerTemplate,
	}
}

func (h *ContainerHandlder) CreateAgent(ctx *gin.Context) {
	var agent types.Agent
	err := ctx.ShouldBindBodyWithJSON(&agent)
	if err != nil {
		ctx.Error(errors.NewBadRequestError("Error mapping body to json", err, &agent))
		return
	}

	container, err := h.dockerTemplate.CreateAgentContainer(ctx, &agent)
	if err != nil {
		ctx.Error(errors.NewServerSideError("Failed to create container", err, &agent))
		return
	}

	err = h.dockerTemplate.StartAgentContainer(ctx, &agent, container)
	if err != nil {
		ctx.Error(errors.NewServerSideError("Failed to start container", err, &agent))
	}
}
