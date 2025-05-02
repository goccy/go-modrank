package repository

import (
	"context"
)

type Option func(*Repository) error

type TokenIssuer func(context.Context) (string, error)

func WithAuthToken(issuer TokenIssuer) Option {
	return func(r *Repository) error {
		r.authTokenIssuer = issuer
		return nil
	}
}

type Cloner interface {
	HeadCommit(ctx context.Context, path string) (string, error)
	Clone(ctx context.Context, path, url string, auth *BasicAuth) error
}

func WithCloner(cloner Cloner) Option {
	return func(r *Repository) error {
		r.cloner = cloner
		return nil
	}
}

func WithClonePath(path string) Option {
	return func(r *Repository) error {
		r.clonePath = path
		return nil
	}
}

func WithWeight(v int) Option {
	return func(r *Repository) error {
		r.weight = v
		return nil
	}
}
