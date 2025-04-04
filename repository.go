package modrank

type RepositoryStatus struct {
	OrgWithName    string
	HeadCommitHash string
	IsArchived     bool
	ExistsGoMod    bool
}
