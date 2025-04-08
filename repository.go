package modrank

type RepositoryStatus struct {
	NameWithOwner  string
	HeadCommitHash string
	IsArchived     bool
	ExistsGoMod    bool
}
