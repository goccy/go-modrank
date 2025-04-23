package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/goccy/go-modrank"
	"github.com/goccy/go-modrank/repository"
)

type BaseOption struct {
	Database     string   `description:"specify the database path for caching" long:"database" short:"d"`
	Organization string   `description:"specify the GitHub Organization to scan all repositories" long:"org" short:"o"`
	Repositories []string `description:"specify the repository address" long:"repository" short:"r"`
	Config       string   `description:"specify the config path" long:"config" short:"c"`
	Worker       int      `description:"specify the worker number for concurrent processing" long:"worker" short:"w" default:"1"`
	Debug        bool     `description:"enable debug log" long:"debug"`
}

type Option struct {
	Run    RunCommand    `description:"scan all repositories and outputs ranking data" command:"run"`
	Update UpdateCommand `description:"update repository status by GitHub API to improve performance" command:"update"`
}

type RunCommand struct {
	*BaseOption
	GitAccessToken    string `description:"specify the access token for private module with go mod graph command" env:"GIT_ACCESS_TOKEN" long:"git-access-token"`
	ClonePath         string `description:"specify the cloned repository base path for caching" long:"clone-path"`
	CleanupRepository bool   `description:"specify deleting the cloned repository after scanning is complete" long:"cleanup-repo"`
	JSON              bool   `description:"output result with JSON format" long:"json"`
}

func (c *RunCommand) Execute(args []string) error {
	ctx := context.Background()
	cfg, err := toConfig(c.BaseOption)
	if err != nil {
		return err
	}
	cfg.ClonePath = c.ClonePath
	cfg.GitAccessToken = c.GitAccessToken
	cfg.CleanupRepository = c.CleanupRepository

	r, repos, err := createModRank(ctx, cfg)
	if err != nil {
		return err
	}
	mods, err := r.Run(ctx, repos...)
	if err != nil {
		return err
	}
	if c.JSON {
		b, err := json.Marshal(mods)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(b))
		return nil
	}
	for idx, mod := range mods {
		fmt.Fprintf(os.Stdout, "- [%d] %s (%s): %d\n", idx+1, mod.Name, mod.Repository, mod.Score)
	}
	return nil
}

type UpdateCommand struct {
	*BaseOption
}

func (c *UpdateCommand) Execute(args []string) error {
	ctx := context.Background()
	cfg, err := toConfig(c.BaseOption)
	if err != nil {
		return err
	}
	r, repos, err := createModRank(ctx, cfg)
	if err != nil {
		return err
	}
	if err := r.UpdateRepositoryStatusByGitHubAPI(ctx, repos...); err != nil {
		return err
	}
	return nil
}

type exitCode int

const (
	exitOK    exitCode = 0
	exitError exitCode = 1
)

var (
	opt         Option
	githubToken = os.Getenv("GITHUB_TOKEN")
)

func main() {
	parser := flags.NewParser(&opt, flags.Default)
	if _, err := parser.Parse(); err != nil {
		if !flags.WroteHelp(err) {
			os.Exit(int(exitError))
		}
	}
	os.Exit(int(exitOK))
}

type Config struct {
	Database          string
	Organization      string
	Repositories      []string
	Worker            int
	Debug             bool
	ClonePath         string
	GitAccessToken    string
	CleanupRepository bool
}

func toConfig(opt *BaseOption) (*Config, error) {
	cfg := &Config{
		Database:     opt.Database,
		Organization: opt.Organization,
		Repositories: opt.Repositories,
		Worker:       opt.Worker,
		Debug:        opt.Debug,
	}
	if opt.Config != "" {
		c, err := modrank.LoadConfig(opt.Config)
		if err != nil {
			return nil, err
		}
		if c.Database != "" {
			cfg.Database = c.Database
		}
		if len(c.Repositories) != 0 {
			cfg.Repositories = c.Repositories
		}
		if c.ClonePath != "" {
			cfg.ClonePath = c.ClonePath
		}
	}
	return cfg, nil
}

func createModRank(ctx context.Context, cfg *Config) (*modrank.ModRank, []*repository.Repository, error) {
	var modrankOpts []modrank.Option
	if cfg.Database != "" {
		modrankOpts = append(modrankOpts, modrank.WithSQLiteDSN(cfg.Database))
	}
	if cfg.Debug {
		modrankOpts = append(modrankOpts, modrank.WithLogLevel(slog.LevelDebug))
	}
	if cfg.GitAccessToken != "" {
		modrankOpts = append(modrankOpts, modrank.WithGitAccessToken(cfg.GitAccessToken))
	}
	if cfg.CleanupRepository {
		modrankOpts = append(modrankOpts, modrank.WithCleanupRepository())
	}
	modrankOpts = append(
		modrankOpts,
		modrank.WithWorker(cfg.Worker),
		modrank.WithGitHubAPICache(),
	)

	repoOpts := []repository.Option{repository.WithAuthToken(githubToken)}
	if cfg.ClonePath != "" {
		repoOpts = append(
			repoOpts,
			repository.WithClonePath(cfg.ClonePath),
		)
	}
	r, err := modrank.New(ctx, modrankOpts...)
	if err != nil {
		return nil, nil, err
	}
	var scanRepos []*repository.Repository
	if cfg.Organization != "" {
		githubClient := modrank.NewGitHubClient(ctx, githubToken)
		repoNames, err := githubClient.FindRepositoriesByOwner(ctx, cfg.Organization)
		if err != nil {
			return nil, nil, err
		}
		for _, repoName := range repoNames {
			repo, err := repository.New(fmt.Sprintf("https://github.com/%s/%s.git", cfg.Organization, repoName), repoOpts...)
			if err != nil {
				return nil, nil, err
			}
			scanRepos = append(scanRepos, repo)
		}
	}
	for _, repo := range cfg.Repositories {
		if !strings.HasPrefix(repo, "https://") {
			repo = "https://" + repo
		}
		if !strings.HasSuffix(repo, ".git") {
			repo += ".git"
		}
		repo, err := repository.New(repo, repoOpts...)
		if err != nil {
			return nil, nil, err
		}
		scanRepos = append(scanRepos, repo)
	}
	if len(scanRepos) == 0 {
		return nil, nil, errors.New("required repository url for scanning")
	}
	return r, scanRepos, nil
}
