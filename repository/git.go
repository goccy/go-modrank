package repository

import (
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

var (
	plainOpen                = git.PlainOpen
	plainCloneContext        = git.PlainCloneContext
	ErrEmptyRemoteRepository = transport.ErrEmptyRemoteRepository
)

type (
	BasicAuth    = http.BasicAuth
	CloneOptions = git.CloneOptions
)
