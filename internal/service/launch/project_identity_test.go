package launch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type projectIdentityRepository struct {
	snapshot ConfigSnapshot
	cause    error
}

func (repository projectIdentityRepository) Project(_ context.Context, _ string, authorize ConfigAuthorization) (ConfigSnapshot, bool, error) {
	if repository.cause != nil {
		return ConfigSnapshot{}, false, repository.cause
	}
	if err := authorize(repository.snapshot.Authority.Source); err != nil {
		return ConfigSnapshot{}, false, err
	}
	return repository.snapshot, true, nil
}

func TestProjectIdentityPreservesProductAndPreviewAuthority(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, state, purpose, delivery string
		allowed                        bool
	}{
		{"product bootstrap", "CREATED", "PRODUCT", "FILE_TREE_PROJECT", true},
		{"product active", "ACTIVE", "PRODUCT", "FILE_TREE_PROJECT", true},
		{"seekable", "ACTIVE", "PRODUCT", "SEEKABLE_PROJECT_ARCHIVE", true},
		{"isolated", "ACTIVE", "PRODUCT", "ISOLATED_WEB_PROJECT", true},
		{"single blob", "ACTIVE", "PRODUCT", "ROM_BLOB", false},
		{"preview bootstrap", "CREATED", "REVIEW_PREVIEW", "FILE_TREE_PROJECT", false},
		{"preview active", "ACTIVE", "REVIEW_PREVIEW", "FILE_TREE_PROJECT", true},
		{"preview finished", "FINISHED", "REVIEW_PREVIEW", "FILE_TREE_PROJECT", false},
		{"product revoked", "REVOKED", "PRODUCT", "FILE_TREE_PROJECT", false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			repository := projectIdentityRepository{snapshot: ConfigSnapshot{
				Authority: ConfigAuthority{Source: ConfigSource{
					State: item.state, Purpose: item.purpose, Delivery: item.delivery, Version: 1, HardEnd: 2000, BootstrapEnd: 1500,
				}},
				Files: []ConfigFile{{LogicalName: "Data/System.json", Format: "RPG_MAKER_PROJECT", Digest: strings.Repeat("a", 64)}},
			}}
			service := NewProjectQueries(repository, func() time.Time { return time.UnixMilli(1000) }, func(capability string, _ []byte) bool { return capability == "valid" })
			identity, err := service.Identity(t.Context(), "launch", "valid")
			if item.allowed && (identity == "" || err != nil) {
				t.Fatalf("valid project rejected: %v", err)
			}
			if !item.allowed && (identity != "" || !errors.Is(err, ErrCredential)) {
				t.Fatalf("unauthorized project accepted: %v", err)
			}
			identity, err = service.Identity(t.Context(), "launch", "wrong")
			if identity != "" || !errors.Is(err, ErrCredential) {
				t.Fatalf("wrong capability accepted: %v", err)
			}
		})
	}
}

func TestProjectIdentityPreservesStorageCause(t *testing.T) {
	t.Parallel()
	service := NewProjectQueries(projectIdentityRepository{cause: context.Canceled}, time.Now, nil)
	identity, err := service.Identity(t.Context(), "launch", "valid")
	if identity != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("project cause lost: %v", err)
	}
}
