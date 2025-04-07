package modrank

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"golang.org/x/mod/modfile"
	"golang.org/x/sync/errgroup"

	"github.com/goccy/go-modrank/internal/helper"
	"github.com/goccy/go-modrank/repository"
)

type ModRank struct {
	storage        Storage
	scoredModCache map[*GoModule]int
	logLevel       slog.Level
	logger         *slog.Logger
	githubToken    string
	githubClient   *GitHubClient
	githubAPICache bool
	workerNum      int
}

const defaultWorkerNum = 1

func New(ctx context.Context, opts ...Option) (*ModRank, error) {
	modRank := &ModRank{
		scoredModCache: make(map[*GoModule]int),
		githubToken:    os.Getenv("GITHUB_TOKEN"),
		workerNum:      defaultWorkerNum,
		logLevel:       slog.LevelInfo,
	}
	for _, opt := range opts {
		if err := opt(modRank); err != nil {
			return nil, err
		}
	}
	modRank.githubClient = NewGitHubClient(ctx, modRank.githubToken)
	modRank.logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: modRank.logLevel,
	}))
	if modRank.storage == nil {
		storage, err := NewSQLiteStorage(filepath.Join(helper.TmpRoot, "tmp.db"))
		if err != nil {
			return nil, err
		}
		modRank.storage = storage
	}
	return modRank, nil
}

type GoModuleScore struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Score      int    `json:"score"`
}

// UpdateRepositoryStatusByGitHubAPI if you are working with a large number of repositories and they are all on GitHub,
// it is useful to skip the process of cloning the repositories by checking in advance
// whether they have been archived or whether they have a go.mod file, and thus shorten the process.
// This API checks for these things and saves them in the database.
func (r *ModRank) UpdateRepositoryStatusByGitHubAPI(ctx context.Context, repos ...*repository.Repository) error {
	ctx = withLogger(ctx, r.logger)
	if err := r.storage.CreateRepositoryStorageIfNotExists(ctx); err != nil {
		return err
	}

	eg, workerCtx := errgroup.WithContext(ctx)
	eg.SetLimit(r.workerNum)

	totalRepoNum := len(repos)
	updatedRepoNum := int32(0)

	if err := r.githubClient.CreateGitHubRepositoryCache(repos); err != nil {
		return err
	}

	for _, repo := range repos {
		eg.Go(func() (e error) {
			defer func() {
				if r := recover(); r != nil {
					logger(workerCtx).WarnContext(workerCtx, "recover error", "error", r)
					e = errors.New(fmt.Sprint(r))
				}
				atomic.AddInt32(&updatedRepoNum, 1)
				curNum := atomic.LoadInt32(&updatedRepoNum)
				ratio := float64(curNum) / float64(totalRepoNum) * 100
				logger(workerCtx).DebugContext(workerCtx, fmt.Sprintf("progress: %d/%d (%.1f%%)", curNum, totalRepoNum, ratio))
			}()
			return r.updateRepositoryStatusByGitHubAPI(ctx, repo)
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	return nil
}

func (r *ModRank) updateRepositoryStatusByGitHubAPI(ctx context.Context, repo *repository.Repository) error {
	if !repo.IsGitHubRepository() {
		return nil
	}
	repoStat, _ := r.storage.FindRepositoryByName(ctx, repo.OrgWithName())
	if repoStat != nil && repoStat.IsArchived {
		logger(ctx).DebugContext(ctx, "skip updating: repository is already archived")
		return nil
	}
	if repoStat != nil && repoStat.ExistsGoMod {
		logger(ctx).DebugContext(ctx, "skip updating: repository has go.mod")
		return nil
	}

	var lastHead string
	if repoStat != nil {
		lastHead = repoStat.HeadCommitHash
	}

	head, err := r.githubClient.GetHeadCommit(ctx, repo.Org(), repo.Name())
	if err != nil {
		return fmt.Errorf("failed to get head commit: %w", err)
	}
	if head != "" && lastHead == head {
		logger(ctx).DebugContext(ctx, "skip updating: HEAD commit is already scanned")
		return nil
	}

	isArchived, err := r.githubClient.IsArchived(ctx, repo.Org(), repo.Name())
	if err != nil {
		return fmt.Errorf("failed to get archived status: %w", err)
	}
	if isArchived {
		logger(ctx).DebugContext(ctx, "save repository status", "isArchived", true)
		if err := r.storage.InsertOrUpdateRepository(ctx, &RepositoryStatus{
			OrgWithName:    repo.OrgWithName(),
			IsArchived:     true,
			HeadCommitHash: head,
		}); err != nil {
			return err
		}
		return nil
	}
	existsGoMod, err := r.githubClient.ExistsGoMod(ctx, repo.Org(), repo.Name())
	if err != nil {
		return fmt.Errorf("failed to find go.mod with unexpected error: %w", err)
	}
	logger(ctx).DebugContext(ctx, "save repository status", "go.mod", existsGoMod)
	if err := r.storage.InsertOrUpdateRepository(ctx, &RepositoryStatus{
		OrgWithName:    repo.OrgWithName(),
		ExistsGoMod:    existsGoMod,
		HeadCommitHash: lastHead, // keep last head value to update scanning process.
	}); err != nil {
		return err
	}
	return nil
}

// Run compute and return the Go module score for each specified repository.
// If UpdateRepositoryStatusByGitHubAPI has been called previously, precomputed statuses can be used to reduce processing time.
func (r *ModRank) Run(ctx context.Context, repos ...*repository.Repository) ([]*GoModuleScore, error) {
	ctx = withLogger(ctx, r.logger)
	if err := r.storage.CreateRepositoryStorageIfNotExists(ctx); err != nil {
		return nil, err
	}
	if err := r.storage.CreateGoModuleStorageIfNotExists(ctx); err != nil {
		return nil, err
	}
	repoMap := make(map[string]*repository.Repository)

	eg, workerCtx := errgroup.WithContext(ctx)
	eg.SetLimit(r.workerNum)

	totalRepoNum := len(repos)
	scannedRepoNum := int32(0)

	if r.githubAPICache {
		if err := r.githubClient.CreateGitHubRepositoryCache(repos); err != nil {
			return nil, err
		}
	}

	for _, repo := range repos {
		repoMap[repo.OrgWithName()] = repo
		eg.Go(func() (e error) {
			defer func() {
				if r := recover(); r != nil {
					logger(workerCtx).WarnContext(workerCtx, "recover error", "error", r)
				}
			}()
			if err := r.scanRepo(
				withLogAttr(
					workerCtx,
					slog.String("repo", repo.URL()),
					slog.String("cloned_path", repo.Path()),
				),
				repo,
			); err != nil {
				logger(workerCtx).WarnContext(workerCtx, "failed to scan repository", "error", err)
			}
			atomic.AddInt32(&scannedRepoNum, 1)
			curNum := atomic.LoadInt32(&scannedRepoNum)
			ratio := float64(curNum) / float64(totalRepoNum) * 100
			logger(workerCtx).DebugContext(workerCtx, fmt.Sprintf("progress: %d/%d (%.1f%%)", curNum, totalRepoNum, ratio))
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	roots, err := r.storage.FindRootGoModules(ctx)
	if err != nil {
		return nil, err
	}
	logger(ctx).DebugContext(ctx, fmt.Sprintf("root module num: %d", len(roots)))
	for idx, root := range roots {
		logger(ctx).DebugContext(ctx, fmt.Sprintf("root module index: %d", idx))
		repo := repoMap[root.Repository]
		if repo == nil {
			return nil, fmt.Errorf("failed to find repository from name: %s", root.Repository)
		}
		depMap := make(map[*GoModule]struct{})
		depMap[root] = struct{}{}
		r.scoredModCache[root] = repo.Weight()
		r.scoreGoModule(ctx, root, repo.Weight()+1, depMap)
	}

	modToScore := make(map[string]*GoModuleScore)
	for mod, score := range r.scoredModCache {
		if _, exists := modToScore[mod.Name]; !exists {
			modToScore[mod.Name] = &GoModuleScore{
				Name:       mod.Name,
				Repository: mod.HostedRepository,
			}
		}
		modToScore[mod.Name].Score += score
	}
	results := make([]*GoModuleScore, 0, len(modToScore))
	for _, mod := range modToScore {
		results = append(results, mod)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	logger(ctx).DebugContext(ctx, fmt.Sprintf("result num: %d", len(results)))
	return results, nil
}

func (r *ModRank) scoreGoModule(ctx context.Context, mod *GoModule, weight int, depMap map[*GoModule]struct{}) {
	for _, ref := range mod.Refers {
		if _, exists := depMap[ref]; exists {
			// found cyclic dependency.
			continue
		}
		depMap[ref] = struct{}{}
		r.scoredModCache[ref] += weight

		r.scoreGoModule(ctx, ref, weight+1, depMap)
	}
}

func (r *ModRank) scanRepo(ctx context.Context, repo *repository.Repository) error {
	repoStat, _ := r.storage.FindRepositoryByName(ctx, repo.OrgWithName())
	if repoStat != nil && repoStat.IsArchived {
		logger(ctx).DebugContext(ctx, "skip scanning: repository is already archived", "from", "db")
		return nil
	}
	if repoStat != nil && !repoStat.ExistsGoMod {
		// UpdateRepositoryStatusByGitHubAPI in advance to allow for the possibility of go.mod being added later.
		logger(ctx).DebugContext(ctx, "skip scanning: repository doesn't have go.mod", "from", "db")
		return nil
	}

	path := repo.Path()
	// If a repository has already been cloned locally and its head commit is stored in the database,
	// it is assumed to have been scanned with that head commit and skipped.
	if head, _ := repo.HeadCommit(ctx, path); head != "" && (repoStat != nil && repoStat.HeadCommitHash == head) {
		logger(ctx).DebugContext(ctx, "skip scanning: HEAD commit is already scanned", "from", "cloned_repo")
		return nil
	}

	if r.githubAPICache && repo.IsGitHubRepository() {
		head, err := r.githubClient.GetHeadCommit(ctx, repo.Org(), repo.Name())
		if err != nil {
			return fmt.Errorf("failed to get head commit: %w", err)
		}
		if head != "" && (repoStat != nil && repoStat.HeadCommitHash == head) {
			logger(ctx).DebugContext(ctx, "skip scanning: HEAD commit is already scanned", "from", "github_api")
			return nil
		}
	}

	logger(ctx).DebugContext(ctx, "cloning repository...")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	if err := repo.Clone(ctx, path); err != nil {
		if err == repository.ErrEmptyRemoteRepository {
			return nil
		}
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	head, err := repo.HeadCommit(ctx, path)
	if err != nil {
		return err
	}
	if head != "" && (repoStat != nil && repoStat.HeadCommitHash == head) {
		logger(ctx).DebugContext(ctx, "skip scanning: HEAD commit is already scanned", "from", "cloned_repo")
		return nil
	}
	logger(ctx).DebugContext(ctx, "scanning...")
	paths, err := repo.GoModPaths()
	if err != nil {
		return err
	}
	eg, childCtx := errgroup.WithContext(ctx)
	for _, path := range paths {
		eg.Go(func() (e error) {
			defer func() {
				if r := recover(); r != nil {
					logger(childCtx).WarnContext(childCtx, "recover error", "error", r)
				}
			}()
			if err := r.scanGoModule(withLogAttr(childCtx, slog.String("go.mod", path)), repo, path); err != nil {
				return err
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	logger(ctx).DebugContext(ctx, "save scanning status", "head", head)
	if err := r.storage.InsertOrUpdateRepository(ctx, &RepositoryStatus{
		OrgWithName:    repo.OrgWithName(),
		HeadCommitHash: head,
		ExistsGoMod:    len(paths) != 0,
	}); err != nil {
		return err
	}
	return nil
}

func (r *ModRank) scanGoModule(ctx context.Context, repo *repository.Repository, path string) error {
	gomod, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	goModFile, err := modfile.Parse(path, gomod, nil)
	if err != nil {
		logger(ctx).WarnContext(ctx, "failed to parse go.mod", "error", err.Error())
		return nil
	}
	modName := goModFile.Module.Mod.Path
	ctx = withLogAttr(ctx, slog.String("modname", modName))

	cmd := exec.CommandContext(ctx, "go", "mod", "graph")
	cmd.Dir = filepath.Dir(path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger(ctx).WarnContext(ctx, "failed to run `go mod graph`", "stdout", string(out), "stderr", err.Error())
		return nil
	}
	modCache := make(map[string]*GoModule)
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) == 0 {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) != 2 {
			logger(ctx).WarnContext(ctx, "unexpected go mod graph format", "line", line)
			return nil
		}
		caller, err := newGoModule(repo, path, modName, parts[0], modCache)
		if err != nil {
			logger(ctx).WarnContext(ctx, "unexpected go module path", "target_mod", parts[0], "error", err.Error())
		}
		callee, err := newGoModule(repo, path, modName, parts[1], modCache)
		if err != nil {
			logger(ctx).WarnContext(ctx, "unexpected go module path", "target_mod", parts[1], "error", err.Error())
		}
		if caller != nil && callee != nil {
			caller.referMap[callee] = struct{}{}
			callee.refererMap[caller] = struct{}{}
		}
	}
	logger(ctx).DebugContext(ctx, fmt.Sprintf("scanned %d modules", len(modCache)))
	mods := make([]*GoModule, 0, len(modCache))
	for _, mod := range modCache {
		mod.setupReference()
		mods = append(mods, mod)
	}
	if err := r.storage.InsertOrUpdateGoModules(ctx, mods); err != nil {
		return err
	}
	return nil
}
