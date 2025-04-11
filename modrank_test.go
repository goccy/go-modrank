package modrank_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/goccy/go-modrank"
	"github.com/goccy/go-modrank/repository"
)

type TestCloner struct {
	headCommit func(ctx context.Context, path string) (string, error)
	clone      func(ctx context.Context, path, url string, auth *repository.BasicAuth) error
}

func (c *TestCloner) HeadCommit(ctx context.Context, path string) (string, error) {
	return c.headCommit(ctx, path)
}

func (c *TestCloner) Clone(ctx context.Context, path, url string, auth *repository.BasicAuth) error {
	return c.clone(ctx, path, url, auth)
}

func TestModRank_UpdateRepositoryStatusByGitHubAPI(t *testing.T) {
	ctx := context.Background()
	r, err := modrank.New(ctx,
		modrank.WithSQLiteDSN(filepath.Join(t.TempDir(), "test.db")),
		modrank.WithLogLevel(slog.LevelDebug),
	)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := repository.New("https://github.com/goccy/go-modrank.git")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateRepositoryStatusByGitHubAPI(ctx, repo); err != nil {
		t.Fatal(err)
	}
}

func TestModRank_Run(t *testing.T) {
	ctx := context.Background()
	r, err := modrank.New(ctx,
		modrank.WithSQLiteDSN(filepath.Join(t.TempDir(), "test.db")),
		modrank.WithLogLevel(slog.LevelDebug),
	)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := repository.New(
		"https://owner/foo.git",
		repository.WithClonePath("testdata"),
		repository.WithCloner(&TestCloner{
			headCommit: func(_ context.Context, _ string) (string, error) {
				return "HEAD", nil
			},
			clone: func(_ context.Context, _, _ string, _ *repository.BasicAuth) error {
				return nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	mods, err := r.Run(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	for idx, mod := range mods {
		t.Logf("- [%d] %s (%s): %d\n", idx+1, mod.Name, mod.Repository, mod.Score)
	}
}
