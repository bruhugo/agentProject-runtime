package config

const (
	PROD = "PROD"
	DEV  = "DEV"
)

type ConfigProvider interface {
	LoadConfig() error
}

func GetConfigProvider(mode string) *ConfigFileProvider {
	switch mode {
	case PROD:
		// TODO: implement later
		return nil
	case DEV:
		return &ConfigFileProvider{}
	case "":
		return &ConfigFileProvider{}
	default:
		panic("invalid config mode passed")
	}
}
