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
	blobstorage := blobstorage.NewS3Bucket()
	dockerTemplate := docker.NewDockerTemplateImpl(blobstorage)

	ctx := context.Background()
	dockerTemplate.Start(ctx)

	// HANDLERS
	containerHandler := handlers.NewContainerHanlder(dockerTemplate)

	v1.POST("/agents", containerHandler.CreateAgent)

	router.Run(":8080")
}
