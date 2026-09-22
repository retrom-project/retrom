package payloadrelease

import (
	"context"
	"errors"
	"fmt"
)

var ErrLifecycleInvariant = errors.New("PAYLOAD_LIFECYCLE_INVARIANT")

type BlobEdge struct{ Table, Column string }

type LifecycleOwner struct {
	Owner                                         Owner
	ReleaseJobID, ReleaseKind, PublicReleaseJobID string
	ReleaseScope                                  Scope
}

type LifecycleReader interface {
	BlobEdges(context.Context) ([]BlobEdge, error)
	Owners(context.Context, Scope, int) ([]LifecycleOwner, error)
}

type LifecycleRepository interface {
	WithLifecycle(context.Context, func(LifecycleReader) error) error
}

type LifecycleVerifier struct{ repository LifecycleRepository }

func NewLifecycleVerifier(repository LifecycleRepository) *LifecycleVerifier {
	return &LifecycleVerifier{repository: repository}
}

func (verifier *LifecycleVerifier) Validate(ctx context.Context) error {
	err := verifier.repository.WithLifecycle(ctx, func(reader LifecycleReader) error {
		edges, err := reader.BlobEdges(ctx)
		if err != nil {
			return fmt.Errorf("read payload ownership registry: %w", err)
		}
		if err := ValidateOwnershipRegistry(edges); err != nil {
			return err
		}
		return validateLifecyclePages(ctx, reader)
	})
	if err != nil {
		return fmt.Errorf("validate payload lifecycle: %w", err)
	}
	return nil
}

func validateLifecyclePages(ctx context.Context, reader LifecycleReader) error {
	var cursor Scope
	for {
		owners, err := reader.Owners(ctx, cursor, 200)
		if err != nil {
			return fmt.Errorf("read payload lifecycle owners: %w", err)
		}
		if len(owners) == 0 {
			return nil
		}
		for _, owner := range owners {
			if err := validateLifecycleOwner(owner); err != nil {
				return err
			}
		}
		next := owners[len(owners)-1].Owner.Scope
		if next.Type < cursor.Type || next.Type == cursor.Type && next.ID <= cursor.ID {
			return ErrLifecycleInvariant
		}
		cursor = next
	}
}

func validateLifecycleOwner(facts LifecycleOwner) error {
	owner := facts.Owner
	if owner.PayloadState == "RETAINED" {
		if !lifecycleTerminal(owner) {
			return nil
		}
		return fmt.Errorf("%w: terminal %s retains payload", ErrLifecycleInvariant, owner.Scope.Type)
	}
	if facts.ReleaseJobID == "" || facts.ReleaseJobID != owner.ReleaseJobID || facts.ReleaseKind != "PAYLOAD_RELEASE" {
		return fmt.Errorf("%w: invalid release job for %s", ErrLifecycleInvariant, owner.Scope.Type)
	}
	expected := owner.Scope
	if (owner.Scope.Type == ScopeSourceImportItem) && owner.PublicID != "" {
		expected = Scope{Type: ScopeImportItem, ID: owner.PublicID}
		if facts.PublicReleaseJobID != owner.ReleaseJobID {
			return fmt.Errorf("%w: unrelated public release", ErrLifecycleInvariant)
		}
	}
	if facts.ReleaseScope != expected {
		return fmt.Errorf("%w: unrelated release scope", ErrLifecycleInvariant)
	}
	return nil
}

func lifecycleTerminal(owner Owner) bool {
	switch owner.Scope.Type {
	case ScopeImportItem:
		return TerminalImportItem(owner.State)
	case ScopeImportJob:
		return TerminalImportJob(owner.State)
	case ScopeSourceImportItem:
		return TerminalSourceItem(owner.State, owner.Retryable)
	case ScopeGame:
		return owner.State == "DELETED"
	case ScopeUploadConsumption, ScopeBlob:
		return false
	default:
		return false
	}
}
