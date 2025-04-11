package modrank

import "log/slog"

type Option func(r *ModRank) error

// WithGitHubToken specify the token for using the GitHub API.
// If this option is not specified, the value of the GITHUB_TOKEN environment variable is used.
func WithGitHubToken(tk string) Option {
	return func(r *ModRank) error {
		r.githubToken = tk
		return nil
	}
}

// WithStorage specify the storage for storing the scan results.
// By default, SQLite is used, but if you want to use another database, you can change this option.
func WithStorage(s Storage) Option {
	return func(r *ModRank) error {
		r.storage = s
		return nil
	}
}

// WithSQLiteDSN set SQLite dsn.
// If this option is not specified, it is stored in the file os.TempDir()/go-modrank/tmp.db.
// If the WithStorage() option is specified, this option is ignored.
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

// WithWorker set the number of workers scanning the repository in concurrent.
// Default is 1 (sequential).
func WithWorker(v int) Option {
	return func(r *ModRank) error {
		r.workerNum = v
		return nil
	}
}

// WithLogger set your logger.
func WithLogger(v *slog.Logger) Option {
	return func(r *ModRank) error {
		r.logger = v
		return nil
	}
}

// WithLogLevel set log level.
// If you configure your logger with WithLogger() option, this option is ignored.
func WithLogLevel(v slog.Level) Option {
	return func(r *ModRank) error {
		r.logLevel = v
		return nil
	}
}

// WithGitHubAPICache use the GitHub API to reduce the time spent scanning repositories as much as possible.
// If you are trying to scan private repositories, you need to set the access token in the GITHUB_TOKEN environment variable or
// specify the token directly in the WithGitHubToken() option.
func WithGitHubAPICache() Option {
	return func(r *ModRank) error {
		r.githubAPICache = true
		return nil
	}
}
