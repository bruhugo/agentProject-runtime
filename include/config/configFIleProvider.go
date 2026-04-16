package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type ConfigFileProvider struct {
}

func (provider *ConfigFileProvider) GetAppConfig() error {
	err := godotenv.Load()
	if err != nil {
		return fmt.Errorf("error loading env variables: %w", err)
	}
	return env.Parse(&AppConfig)
}
