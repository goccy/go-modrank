package modrank

import (
	"context"
	"log/slog"
)

type loggerKey struct{}

func withLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

func withLogAttr(ctx context.Context, attrs ...any) context.Context {
	logger := ctx.Value(loggerKey{})
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerKey{}, logger.(*slog.Logger).With(attrs...))
}

func logger(ctx context.Context) *slog.Logger {
	logger := ctx.Value(loggerKey{})
	return logger.(*slog.Logger)
}
