package modrank

import (
	"context"
	"database/sql"
	"encoding/json"

	_ "github.com/glebarez/go-sqlite"
)

var _ Storage = new(SQLiteStorage)

type SQLiteStorage struct {
	db       *sql.DB
	modCache map[string]*GoModule
}

func NewSQLiteStorage(dsn string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	return &SQLiteStorage{
		db:       db,
		modCache: make(map[string]*GoModule),
	}, nil
}

func (s *SQLiteStorage) CreateRepositoryStorageIfNotExists(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`
CREATE TABLE IF NOT EXISTS Repositories (
  NameWithOwner TEXT PRIMARY KEY NOT NULL,
  Head TEXT NOT NULL,
  IsArchived BOOL NOT NULL,
  ExistsGoMod BOOL NOT NULL
)`,
	); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStorage) FindRepositoryByName(ctx context.Context, nameWithOwner string) (*RepositoryStatus, error) {
	var (
		headCommitHash string
		isArchived     bool
		existsGoMod    bool
	)
	if err := s.db.QueryRowContext(
		ctx, "SELECT Head, IsArchived, ExistsGoMod FROM Repositories WHERE NameWithOwner = ?", nameWithOwner,
	).Scan(&headCommitHash, &isArchived, &existsGoMod); err != nil {
		return nil, err
	}
	return &RepositoryStatus{
		NameWithOwner:  nameWithOwner,
		HeadCommitHash: headCommitHash,
		IsArchived:     isArchived,
		ExistsGoMod:    existsGoMod,
	}, nil
}

func (s *SQLiteStorage) InsertOrUpdateRepository(ctx context.Context, st *RepositoryStatus) error {
	if _, err := s.db.ExecContext(
		ctx, `
INSERT INTO
  Repositories(NameWithOwner, Head, IsArchived, ExistsGoMod) VALUES (?, ?, ?, ?)
ON CONFLICT(NameWithOwner)
DO UPDATE
  SET Head = ?, IsArchived = ?, ExistsGoMod = ?
`,
		st.NameWithOwner, st.HeadCommitHash, st.IsArchived, st.ExistsGoMod,

		st.HeadCommitHash, st.IsArchived, st.ExistsGoMod,
	); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStorage) CreateGoModuleStorageIfNotExists(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`
CREATE TABLE IF NOT EXISTS GoModules (
  ID TEXT PRIMARY KEY NOT NULL,
  NameWithOwner TEXT NOT NULL,
  GoModPath TEXT NOT NULL,
  ModuleName TEXT NOT NULL,
  ModuleVersion TEXT NOT NULL,
  HostedRepository TEXT NOT NULL,
  IsRoot BOOL NOT NULL,
  Refers JSON NOT NULL,
  Referers JSON NOT NULL
)`,
	); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStorage) FindRootGoModules(ctx context.Context) ([]*GoModule, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT ID, NameWithOwner, GoModPath, ModuleName, ModuleVersion, HostedRepository, Refers, Referers
           FROM GoModules WHERE IsRoot = TRUE`,
	)
	if err != nil {
		return nil, err
	}
	var roots []*GoModule
	for rows.Next() {
		var (
			id             string
			nameWithOwner  string
			goModPath      string
			modName        string
			modVer         string
			hostedRepo     string
			referIDsJSON   string
			refererIDsJSON string
		)
		if err := rows.Scan(&id, &nameWithOwner, &goModPath, &modName, &modVer, &hostedRepo, &referIDsJSON, &refererIDsJSON); err != nil {
			break
		}
		rootMod := &GoModule{
			ID:               id,
			Repository:       nameWithOwner,
			GoModPath:        goModPath,
			Name:             modName,
			Version:          modVer,
			HostedRepository: hostedRepo,
		}
		s.modCache[id] = rootMod

		refers, err := s.findModulesFromJSON(ctx, referIDsJSON)
		if err != nil {
			return nil, err
		}
		referers, err := s.findModulesFromJSON(ctx, refererIDsJSON)
		if err != nil {
			return nil, err
		}
		rootMod.Refers = refers
		rootMod.Referers = referers
		roots = append(roots, rootMod)
	}
	return roots, nil
}

func (s *SQLiteStorage) FindGoModuleByID(ctx context.Context, id string) (*GoModule, error) {
	if mod, exists := s.modCache[id]; exists {
		return mod, nil
	}

	var (
		nameWithOwner  string
		goModPath      string
		modName        string
		modVer         string
		hostedRepo     string
		referIDsJSON   string
		refererIDsJSON string
	)
	if err := s.db.QueryRowContext(ctx,
		`SELECT NameWithOwner, GoModPath, ModuleName, ModuleVersion, HostedRepository, Refers, Referers
           FROM GoModules WHERE ID = ?`, id,
	).Scan(&nameWithOwner, &goModPath, &modName, &modVer, &hostedRepo, &referIDsJSON, &refererIDsJSON); err != nil {
		return nil, err
	}
	mod := &GoModule{
		ID:               id,
		Repository:       nameWithOwner,
		GoModPath:        goModPath,
		Name:             modName,
		Version:          modVer,
		HostedRepository: hostedRepo,
	}
	s.modCache[id] = mod

	refers, err := s.findModulesFromJSON(ctx, referIDsJSON)
	if err != nil {
		return nil, err
	}
	referers, err := s.findModulesFromJSON(ctx, refererIDsJSON)
	if err != nil {
		return nil, err
	}
	mod.Refers = refers
	mod.Referers = referers
	return mod, nil
}

func (s *SQLiteStorage) InsertOrUpdateGoModules(ctx context.Context, mods []*GoModule) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, mod := range mods {
		if err := s.insertOrUpdateGoModule(ctx, tx, mod); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStorage) insertOrUpdateGoModule(ctx context.Context, tx *sql.Tx, mod *GoModule) error {
	refers := mod.Refers
	referers := mod.Referers
	referIDs := make([]string, 0, len(refers))
	for _, ref := range refers {
		referIDs = append(referIDs, ref.ID)
	}
	refererIDs := make([]string, 0, len(referers))
	for _, ref := range referers {
		refererIDs = append(refererIDs, ref.ID)
	}
	referIDsJSON, err := json.Marshal(referIDs)
	if err != nil {
		return err
	}
	refererIDsJSON, err := json.Marshal(refererIDs)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(
		ctx, `
INSERT INTO
  GoModules(
    ID,
    NameWithOwner,
    GoModPath,
    ModuleName,
    ModuleVersion,
    HostedRepository,
    IsRoot,
    Refers,
    Referers
  ) VALUES (
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?
  )
ON CONFLICT(ID)
  DO UPDATE
    SET IsRoot = ?, Refers = ?, Referers = ?
`,
		mod.ID, mod.Repository, mod.GoModPath, mod.Name, mod.Version, mod.HostedRepository,
		mod.IsRoot(), string(referIDsJSON), string(refererIDsJSON),

		mod.IsRoot(), string(referIDsJSON), string(refererIDsJSON),
	); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStorage) findModulesFromJSON(ctx context.Context, jsonText string) ([]*GoModule, error) {
	var ids []string
	if err := json.Unmarshal([]byte(jsonText), &ids); err != nil {
		return nil, err
	}
	mods := make([]*GoModule, 0, len(ids))
	for _, id := range ids {
		mod, err := s.FindGoModuleByID(ctx, id)
		if err != nil {
			return nil, err
		}
		mods = append(mods, mod)
	}
	return mods, nil
}
