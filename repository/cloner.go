package repository

import (
	"context"
	"fmt"
	"os"
)

type DefaultCloner struct{}

func (c *DefaultCloner) HeadCommit(_ context.Context, path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}

	repo, err := plainOpen(path)
	if err != nil {
		return "", fmt.Errorf("failed to open repository from %s: %w", path, err)
	}
	head, err := repo.Head()
	if err != nil {
		return "", err
	}
	return head.Hash().String(), nil
}

func (c *DefaultCloner) Clone(ctx context.Context, path, url string, auth *BasicAuth) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	if _, err := plainCloneContext(ctx, path, false, &CloneOptions{
		URL:   url,
		Auth:  auth,
		Depth: 1,
	}); err != nil {
		return err
	}
	return nil
}
