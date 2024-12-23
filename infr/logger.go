package infr

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func NewLogger(isDebug bool) *zap.Logger {
	var logger *zap.Logger

	if isDebug {
		config := zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		logger, _ = config.Build()
	} else {
		logger, _ = zap.NewProduction()
	}

	return logger
}
