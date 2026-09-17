package tagging

import (
	"context"
	"fmt"

	model "retrom/internal/model/tagging"
	"retrom/internal/repo/dbexec"
)

// crossDomainWriter implements model.CrossDomainWriter using an executor
// bound to a caller-owned transaction.
type crossDomainWriter struct{ executor dbexec.Executor }

// BindCrossDomain returns a CrossDomainWriter bound to the given executor.
// The executor is typically a *sql.Tx owned by another repo's transaction.
func BindCrossDomain(executor dbexec.Executor) model.CrossDomainWriter {
	return &crossDomainWriter{executor: executor}
}

func (w *crossDomainWriter) ValidateActiveReferences(
	ctx context.Context, tagIDs []string,
) ([]model.Reference, error) {
	return validateActiveReferences(ctx, w.executor, tagIDs)
}

func (w *crossDomainWriter) ReplaceOwnerReferences(
	ctx context.Context, owner model.Owner, tagIDs []string,
	actorUserID string, now int64,
) ([]model.Reference, []model.Reference, error) {
	return replaceOwnerReferences(ctx, w.executor, owner, tagIDs, actorUserID, now)
}

func (w *crossDomainWriter) AssignReferences(
	ctx context.Context, owner model.Owner, refs []model.Reference,
	actorUserID string, now int64,
) error {
	return assignReferences(ctx, w.executor, owner, refs, actorUserID, now)
}

func (w *crossDomainWriter) ReadOwnerReferences(
	ctx context.Context, owner model.Owner,
) ([]model.Reference, error) {
	return readOwnerReferences(ctx, w.executor, owner)
}

func (w *crossDomainWriter) CopyOwnerReferences(
	ctx context.Context, from, to model.Owner,
	actorUserID string, now int64,
) ([]model.Reference, error) {
	return copyOwnerReferences(ctx, w.executor, from, to, actorUserID, now)
}

// Package-level functions for direct use by repo callers.

func validateActiveReferences(
	ctx context.Context, executor dbexec.Executor, tagIDs []string,
) ([]model.Reference, error) {
	validated, err := model.ValidateIDs(tagIDs)
	if err != nil {
		return nil, fmt.Errorf("validate IDs: %w", err)
	}
	if len(validated) == 0 {
		return []model.Reference{}, nil
	}
	scope := writeScope(executor)
	result, err := scope.Tags.ActiveReferences(ctx, validated)
	if err != nil {
		return nil, fmt.Errorf("tagging: read active references: %w", err)
	}
	refs, factsErr := model.ValidateActiveReferenceFacts(validated, result)
	if factsErr != nil {
		return nil, fmt.Errorf("validate active references: %w", factsErr)
	}
	return refs, nil
}

func replaceOwnerReferences(
	ctx context.Context, executor dbexec.Executor,
	owner model.Owner, tagIDs []string,
	actorUserID string, now int64,
) ([]model.Reference, []model.Reference, error) {
	desired, err := validateActiveReferences(ctx, executor, tagIDs)
	if err != nil {
		return nil, nil, err
	}
	scope := writeScope(executor)
	before, err := scope.Relations.References(ctx, owner)
	if err != nil {
		return nil, nil, fmt.Errorf("tagging: read owner references: %w", err)
	}
	plan, err := model.BuildReplacementPlan(
		owner, before, desired, actorUserID, now,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("build replacement plan: %w", err)
	}
	if err := ApplyReplacementPlan(ctx, executor, plan); err != nil {
		return nil, nil, err
	}
	return plan.Before, plan.After, nil
}

func assignReferences(
	ctx context.Context, executor dbexec.Executor,
	owner model.Owner, refs []model.Reference,
	actorUserID string, now int64,
) error {
	if len(refs) == 0 {
		return nil
	}
	if !model.ValidID(owner.ID) || !model.ValidID(actorUserID) {
		return model.ErrInvalid
	}
	scope := writeScope(executor)
	if err := scope.Relations.Add(ctx, model.Assignment{
		Owner:       owner,
		References:  refs,
		ActorUserID: actorUserID,
		NowMS:       now,
	}); err != nil {
		return fmt.Errorf("tagging: assign references: %w", err)
	}
	if err := scope.Relations.TouchTags(
		ctx, actorUserID, model.ReferenceIDs(refs), now,
	); err != nil {
		return fmt.Errorf("tagging: touch assigned tags: %w", err)
	}
	return nil
}

func readOwnerReferences(
	ctx context.Context, executor dbexec.Executor, owner model.Owner,
) ([]model.Reference, error) {
	scope := writeScope(executor)
	refs, err := scope.Relations.References(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("tagging: read references: %w", err)
	}
	return refs, nil
}

func copyOwnerReferences(
	ctx context.Context, executor dbexec.Executor,
	from, to model.Owner,
	actorUserID string, now int64,
) ([]model.Reference, error) {
	if !model.ValidID(from.ID) || !model.ValidID(to.ID) || !model.ValidID(actorUserID) {
		return nil, model.ErrInvalid
	}
	refs, err := readOwnerReferences(ctx, executor, from)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return refs, nil
	}
	if err := assignReferences(ctx, executor, to, refs, actorUserID, now); err != nil {
		return nil, err
	}
	return refs, nil
}
