package tagging

import (
	"context"
	"fmt"

	model "retrom/internal/model/tagging"
	"retrom/internal/repo/dbexec"
)

// Relations provides atomic tagging operations for callers in other repo
// packages that hold their own transaction. The executor field is unexported
// to prevent upper layers from obtaining transaction-bound write capability.
type Relations struct{ executor dbexec.Executor }

// BindCrossDomain returns a *Relations bound to the given executor. The
// concrete type satisfies model.CrossDomainWriter via Go's implicit interface
// matching; callers in repo use the concrete type directly.
func BindCrossDomain(executor dbexec.Executor) *Relations {
	return &Relations{executor: executor}
}

func (r *Relations) ValidateActiveReferences(
	ctx context.Context, tagIDs []string,
) ([]model.Reference, error) {
	return validateActiveReferences(ctx, r.executor, tagIDs)
}

func (r *Relations) ReplaceOwnerReferences(
	ctx context.Context, owner model.Owner, tagIDs []string,
	actorUserID string, now int64,
) ([]model.Reference, []model.Reference, error) {
	return replaceOwnerReferences(ctx, r.executor, owner, tagIDs, actorUserID, now)
}

func (r *Relations) AssignReferences(
	ctx context.Context, owner model.Owner, refs []model.Reference,
	actorUserID string, now int64,
) error {
	return assignReferences(ctx, r.executor, owner, refs, actorUserID, now)
}

func (r *Relations) ReadOwnerReferences(
	ctx context.Context, owner model.Owner,
) ([]model.Reference, error) {
	return readOwnerReferences(ctx, r.executor, owner)
}

func (r *Relations) CopyOwnerReferences(
	ctx context.Context, from, to model.Owner,
	actorUserID string, now int64,
) ([]model.Reference, error) {
	return copyOwnerReferences(ctx, r.executor, from, to, actorUserID, now)
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
	scope := newWriteScope(executor)
	result, err := scope.tags.ActiveReferences(ctx, validated)
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
	scope := newWriteScope(executor)
	before, err := scope.relations.References(ctx, owner)
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
	scope := newWriteScope(executor)
	if err := scope.relations.Add(ctx, model.Assignment{
		Owner:       owner,
		References:  refs,
		ActorUserID: actorUserID,
		NowMS:       now,
	}); err != nil {
		return fmt.Errorf("tagging: assign references: %w", err)
	}
	if err := scope.relations.TouchTags(
		ctx, actorUserID, model.ReferenceIDs(refs), now,
	); err != nil {
		return fmt.Errorf("tagging: touch assigned tags: %w", err)
	}
	return nil
}

func readOwnerReferences(
	ctx context.Context, executor dbexec.Executor, owner model.Owner,
) ([]model.Reference, error) {
	scope := newWriteScope(executor)
	refs, err := scope.relations.References(ctx, owner)
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
	if !model.ValidID(from.ID) || !model.ValidID(to.ID) {
		return nil, model.ErrInvalid
	}
	refs, err := readOwnerReferences(ctx, executor, from)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return refs, nil
	}
	if !model.ValidID(actorUserID) {
		return nil, model.ErrInvalid
	}
	if err := assignReferences(ctx, executor, to, refs, actorUserID, now); err != nil {
		return nil, err
	}
	return refs, nil
}
