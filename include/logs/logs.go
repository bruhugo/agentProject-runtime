package logs

import (
	"log/slog"
	"os"

	"github.com/bruhugo/PicoClawProjectRuntime/include/config"
)

func ConfigureLogs(env string) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: getLogLevel(),
	}))

	slog.SetDefault(logger)
}

func getLogLevel() slog.Level {
	switch config.AppConfig.LogLevel {
	case "ERROR":
		return slog.LevelError
	case "WARNING":
		return slog.LevelWarn
	case "INFO":
		return slog.LevelInfo
	case "DEBUG":
		return slog.LevelDebug
	case "":
		return slog.LevelDebug
	default:
		panic("invalid debug level provided")
	}
}
