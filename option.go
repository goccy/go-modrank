package modrank

import "log/slog"

type Option func(r *ModRank) error

func WithGitHubToken(tk string) Option {
	return func(r *ModRank) error {
		r.githubToken = tk
		return nil
	}
}

func WithStorage(s Storage) Option {
	return func(r *ModRank) error {
		r.storage = s
		return nil
	}
}

func WithSQLiteDSN(dsn string) Option {
	return func(r *ModRank) error {
		s, err := NewSQLiteStorage(dsn)
		if err != nil {
			return err
		}
		r.storage = s
		return nil
	}
}

func WithWorker(v int) Option {
	return func(r *ModRank) error {
		r.workerNum = v
		return nil
	}
}

func WithLogLevel(v slog.Level) Option {
	return func(r *ModRank) error {
		r.logLevel = v
		return nil
	}
}

func WithGitHubAPICache() Option {
	return func(r *ModRank) error {
		r.githubAPICache = true
		return nil
	}
}
