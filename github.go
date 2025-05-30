package modrank

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/go-github/v70/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"

	"github.com/goccy/go-modrank/repository"
)

type GitHubClient struct {
	githubAccessToken *GitHubAccessToken
	repoCache         map[string]*GitHubRepository
	repoCacheMu       sync.RWMutex
}

type GitHubRepository struct {
	Repository *repository.Repository
	IsArchived bool
	HeadCommit string
}

func NewGitHubClient(ctx context.Context, token *GitHubAccessToken) *GitHubClient {
	return &GitHubClient{
		githubAccessToken: token,
		repoCache:         make(map[string]*GitHubRepository),
	}
}

func (c *GitHubClient) FindRepositoriesByOwner(ctx context.Context, owner string) ([]string, error) {
	var repoNames []string
	var cursor *githubv4.String

	for {
		var query struct {
			Organization struct {
				Repositories struct {
					Nodes []struct {
						Name       string
						IsArchived bool
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   *githubv4.String
					}
				} `graphql:"repositories(first: 100, after: $cursor)"`
			} `graphql:"organization(login: $organization)"`
		}

		variables := map[string]interface{}{
			"organization": githubv4.String(owner),
			"cursor":       cursor,
		}

		tk, err := c.githubAccessToken.issuer(ctx)
		if err != nil {
			return nil, fmt.Errorf("modrank: failed to issue GitHub API access token: %w", err)
		}
		gqlClient := githubv4.NewClient(
			oauth2.NewClient(
				ctx,
				oauth2.StaticTokenSource(&oauth2.Token{
					AccessToken: tk,
				}),
			),
		)
		if err := gqlClient.Query(ctx, &query, variables); err != nil {
			return nil, err
		}

		for _, node := range query.Organization.Repositories.Nodes {
			if node.IsArchived {
				continue
			}
			repoNames = append(repoNames, node.Name)
		}
		if !query.Organization.Repositories.PageInfo.HasNextPage {
			break
		}
		cursor = query.Organization.Repositories.PageInfo.EndCursor
	}
	return repoNames, nil
}

func (c *GitHubClient) IsArchived(ctx context.Context, owner, repo string) (bool, error) {
	key := fmt.Sprintf("%s/%s", owner, repo)
	githubRepo := c.getRepositoryFromCache(key)
	if githubRepo == nil {
		return false, errors.New("cannot use IsArchived unless you create a cache in advance by CreateGitHubRepositoryCache")
	}
	return githubRepo.IsArchived, nil
}

func (c *GitHubClient) GetHeadCommit(ctx context.Context, owner, repo string) (string, error) {
	key := fmt.Sprintf("%s/%s", owner, repo)
	githubRepo := c.getRepositoryFromCache(key)
	if githubRepo == nil {
		return "", errors.New("cannot use GetHeadCommit unless you create a cache in advance by CreateGitHubRepositoryCache")
	}
	return githubRepo.HeadCommit, nil
}

func (c *GitHubClient) ExistsGoMod(ctx context.Context, owner, repo string) (bool, error) {
	head, err := c.GetHeadCommit(ctx, owner, repo)
	if err != nil {
		return false, err
	}
	if head == "" {
		return false, nil
	}
	tk, err := c.githubAccessToken.issuer(ctx)
	if err != nil {
		return false, fmt.Errorf("modrank: failed to issue GitHub API access token: %w", err)
	}
	restClient := github.NewClient(
		oauth2.NewClient(
			ctx,
			oauth2.StaticTokenSource(&oauth2.Token{
				AccessToken: tk,
			}),
		),
	)
	tree, _, err := restClient.Git.GetTree(ctx, owner, repo, head, true)
	if err != nil {
		errRes, ok := err.(*github.ErrorResponse)
		if ok {
			if errRes.Response.StatusCode == http.StatusNotFound {
				return false, nil
			}
		}
		return false, fmt.Errorf("failed to get tree from head commit of default branch: %w", err)
	}
	for _, entry := range tree.Entries {
		if entry.GetType() == "blob" && filepath.Base(entry.GetPath()) == "go.mod" {
			return true, nil
		}
	}
	return false, nil
}

func (c *GitHubClient) CreateGitHubRepositoryCache(ctx context.Context, repos []*repository.Repository) error {
	githubRepos := make([]*repository.Repository, 0, len(repos))
	for _, repo := range repos {
		if !repo.IsGitHubRepository() {
			continue
		}
		githubRepos = append(githubRepos, repo)
	}
	var eg errgroup.Group
	for _, chunk := range c.chunkRepos(githubRepos) {
		eg.Go(func() error {
			return c.createGitHubRepositoryCache(ctx, chunk)
		})
	}
	return eg.Wait()
}

func (c *GitHubClient) createGitHubRepositoryCache(ctx context.Context, repos []*repository.Repository) error {
	const githubAPI = "https://api.github.com/graphql"

	var (
		queries  []string
		queryMap = make(map[string]*repository.Repository)
	)
	for _, repo := range repos {
		nameWithOwner := repo.NameWithOwner()
		key := strings.ReplaceAll(nameWithOwner, "/", "_")
		key = strings.ReplaceAll(key, "-", "_")
		key = strings.ReplaceAll(key, ".", "_")
		queryMap[key] = repo
		queries = append(queries, fmt.Sprintf(`
  %s: repository(owner: "%s", name: "%s") {
    name
    isArchived
    defaultBranchRef {
      target {
        oid
      }
    }
  }
`, key, repo.Owner(), repo.Name()),
		)
	}

	query := fmt.Sprintf(`
query {
  %s
}`, strings.Join(queries, "\n"))

	gqlBody := map[string]string{"query": query}
	gqlBodyBytes, err := json.Marshal(gqlBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", githubAPI, bytes.NewBuffer(gqlBodyBytes))
	if err != nil {
		return err
	}
	tk, err := c.githubAccessToken.issuer(ctx)
	if err != nil {
		return fmt.Errorf("modrank: failed to issue GitHub API access token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tk)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to GitHub API: %s: %s", resp.Status, string(body))
	}

	var result struct {
		Data map[string]struct {
			Name             string `json:"name"`
			IsArchived       bool   `json:"isArchived"`
			DefaultBranchRef struct {
				Target struct {
					OID string `json:"oid"`
				} `json:"target"`
			} `json:"defaultBranchRef"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	for key, stat := range result.Data {
		repo, exists := queryMap[key]
		if !exists {
			return fmt.Errorf("failed to find repository from %s", key)
		}
		c.setRepositoryCache(&GitHubRepository{
			Repository: repo,
			IsArchived: stat.IsArchived,
			HeadCommit: stat.DefaultBranchRef.Target.OID,
		})
	}
	return nil
}

const chunkSize = 100

func (c *GitHubClient) chunkRepos(repos []*repository.Repository) [][]*repository.Repository {
	var chunks [][]*repository.Repository
	for i := 0; i < len(repos); i += chunkSize {
		end := i + chunkSize
		if end > len(repos) {
			end = len(repos)
		}
		chunks = append(chunks, repos[i:end])
	}
	return chunks
}

func (c *GitHubClient) getRepositoryFromCache(nameWithOwner string) *GitHubRepository {
	c.repoCacheMu.RLock()
	repo := c.repoCache[nameWithOwner]
	c.repoCacheMu.RUnlock()
	return repo
}

func (c *GitHubClient) setRepositoryCache(repo *GitHubRepository) {
	c.repoCacheMu.Lock()
	c.repoCache[repo.Repository.NameWithOwner()] = repo
	c.repoCacheMu.Unlock()
}
