package payloadrelease

import (
	"context"
	"fmt"
	model "retrom/internal/model/payloadrelease"
)

type LifecycleVerifier struct{ repository model.LifecycleRepository }

func NewLifecycleVerifier(repository model.LifecycleRepository) *LifecycleVerifier {
	return &LifecycleVerifier{repository: repository}
}

func (verifier *LifecycleVerifier) Validate(ctx context.Context) error {
	err := verifier.repository.WithLifecycle(ctx, func(reader model.LifecycleReader) error {
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

func validateLifecyclePages(ctx context.Context, reader model.LifecycleReader) error {
	var cursor model.Scope
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
			return model.ErrLifecycleInvariant
		}
		cursor = next
	}
}

func validateLifecycleOwner(facts model.LifecycleOwner) error {
	owner := facts.Owner
	if owner.PayloadState == "RETAINED" {
		if !lifecycleTerminal(owner) {
			return nil
		}
		return fmt.Errorf("%w: terminal %s retains payload", model.ErrLifecycleInvariant, owner.Scope.Type)
	}
	if facts.ReleaseJobID == "" || facts.ReleaseJobID != owner.ReleaseJobID || facts.ReleaseKind != "PAYLOAD_RELEASE" {
		return fmt.Errorf("%w: invalid release job for %s", model.ErrLifecycleInvariant, owner.Scope.Type)
	}
	expected := owner.Scope
	if (owner.Scope.Type == model.ScopePegasusImportItem ||
		owner.Scope.Type == model.ScopeEmulationStationImportItem) && owner.PublicID != "" {
		expected = model.Scope{Type: model.ScopeImportItem, ID: owner.PublicID}
		if facts.PublicReleaseJobID != owner.ReleaseJobID {
			return fmt.Errorf("%w: unrelated public release", model.ErrLifecycleInvariant)
		}
	}
	if facts.ReleaseScope != expected {
		return fmt.Errorf("%w: unrelated release scope", model.ErrLifecycleInvariant)
	}
	return nil
}

func lifecycleTerminal(owner model.Owner) bool {
	switch owner.Scope.Type {
	case model.ScopeImportItem:
		return model.TerminalImportItem(owner.State)
	case model.ScopeImportJob:
		return model.TerminalImportJob(owner.State)
	case model.ScopePegasusImportItem, model.ScopeEmulationStationImportItem:
		return model.TerminalSourceItem(owner.State, owner.Retryable)
	case model.ScopeGame:
		return owner.State == "DELETED"
	case model.ScopeUploadConsumption, model.ScopeBlob:
		return false
	default:
		return false
	}
}
