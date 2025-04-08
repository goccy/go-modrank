package modrank

import "context"

type Storage interface {
	RepositoryStorage
	GoModuleStorage
}

type RepositoryStorage interface {
	CreateRepositoryStorageIfNotExists(ctx context.Context) error
	FindRepositoryByName(ctx context.Context, nameWithOwner string) (*RepositoryStatus, error)
	InsertOrUpdateRepository(ctx context.Context, st *RepositoryStatus) error
}

type GoModuleStorage interface {
	CreateGoModuleStorageIfNotExists(ctx context.Context) error
	FindRootGoModules(ctx context.Context) ([]*GoModule, error)
	FindGoModuleByID(ctx context.Context, id string) (*GoModule, error)
	InsertOrUpdateGoModules(ctx context.Context, mods []*GoModule) error
}
