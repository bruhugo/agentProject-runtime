package main

import (
	"context"
	"os"

	"github.com/bruhugo/PicoClawProjectRuntime/include/blobstorage"
	"github.com/bruhugo/PicoClawProjectRuntime/include/config"
	"github.com/bruhugo/PicoClawProjectRuntime/include/docker"
	"github.com/bruhugo/PicoClawProjectRuntime/include/errors"
	"github.com/bruhugo/PicoClawProjectRuntime/include/handlers"
	"github.com/bruhugo/PicoClawProjectRuntime/include/logs"
	"github.com/gin-gonic/gin"
)

func main() {
	env := os.Getenv("ENV")
	configProvider := config.GetConfigProvider(env)
	err := configProvider.GetAppConfig()
	if err != nil {
		panic(err.Error())
	}

	logs.ConfigureLogs(env)

	router := gin.Default()
	api := router.Group("/api")
	v1 := api.Group("/v1")

	// MIDDLEWARE
	v1.Use(errors.ErrorHandler())

	// DEPENDENCIES
	ctx := context.Background()
	blobstorage := blobstorage.NewS3Bucket()
	dockerTemplate := docker.NewDockerTemplateImpl(blobstorage, ctx)

	dockerTemplate.Start()

	// HANDLERS
	agentHandler := handlers.NewAgentHanlder(dockerTemplate)

	v1.POST("/agents", agentHandler.CreateAgent)
	v1.PUT("/agents/:agentId/stop", agentHandler.StopAgent)
	v1.GET("/agents/:agentId/stats", agentHandler.GetAgentStats)

	// makes the agent load config file from blobstorage
	v1.PUT("/agents/:agentId/config", agentHandler.UpdadeConfigFile)

	router.Run(":9090")
}
