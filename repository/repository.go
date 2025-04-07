package repository

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/goccy/go-modrank/internal/helper"
)

const DefaultRepositoryWeight = 1

type Repository struct {
	repoName  string
	orgName   string
	url       string
	auth      *BasicAuth
	cloner    Cloner
	clonePath string
	weight    int
}

func New(url string, opts ...Option) (*Repository, error) {
	trimmedExt := strings.TrimSuffix(url, ".git")
	parts := strings.Split(trimmedExt, "/")
	if len(parts) < 3 {
		return nil, fmt.Errorf("unexpected repository url: %s", url)
	}
	repoName := parts[len(parts)-1]
	orgName := parts[len(parts)-2]
	r := &Repository{
		repoName:  repoName,
		orgName:   orgName,
		url:       url,
		cloner:    new(DefaultCloner),
		clonePath: helper.TmpRoot,
		weight:    DefaultRepositoryWeight,
	}
	for _, opt := range opts {
		if err := opt(r); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Repository) Path() string {
	return filepath.Join(r.clonePath, r.repoName)
}

func (r *Repository) Org() string {
	return r.orgName
}

func (r *Repository) Name() string {
	return r.repoName
}

func (r *Repository) OrgWithName() string {
	return r.orgName + "/" + r.repoName
}

func (r *Repository) Auth() *BasicAuth {
	return r.auth
}

func (r *Repository) Weight() int {
	return r.weight
}

func (r *Repository) URL() string {
	return r.url
}

func (r *Repository) HeadCommit(ctx context.Context, path string) (string, error) {
	return r.cloner.HeadCommit(ctx, path)
}

func (r *Repository) Clone(ctx context.Context, path string) error {
	return r.cloner.Clone(ctx, path, r.url, r.auth)
}

func (r *Repository) GoModPaths() ([]string, error) {
	var paths []string
	if err := filepath.Walk(r.Path(), func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// ignore vendor path.
		if r.isVendorPath(path) {
			return nil
		}
		if filepath.Base(path) == "go.mod" {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to get go.mod paths: %w", err)
	}
	return paths, nil
}

func (r *Repository) IsGitHubRepository() bool {
	parsedURL, err := url.Parse(r.url)
	if err != nil {
		return false
	}
	return parsedURL.Host == "github.com"
}

func (r *Repository) isVendorPath(path string) bool {
	for _, sub := range strings.Split(path, string(filepath.Separator)) {
		if sub == "vendor" {
			return true
		}
	}
	return false
}
