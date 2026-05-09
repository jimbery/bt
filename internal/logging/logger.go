package logging

import "go.uber.org/zap"

// New returns a production zap logger. Callers should defer logger.Sync().
func New() (*zap.Logger, error) {
	return zap.NewProduction()
}
