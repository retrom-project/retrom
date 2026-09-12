package launch

import (
	"context"
	"fmt"
	"time"
)

type ProjectIndexes struct {
	repository ProjectIndexReader
	policy     accessPolicy
}

func NewProjectIndexes(repository ProjectIndexReader, now func() time.Time, matches MatchCapability) *ProjectIndexes {
	return &ProjectIndexes{repository: repository, policy: accessPolicy{now: now, matches: matches}}
}

func (service *ProjectIndexes) Index(
	ctx context.Context,
	ref ProjectIndexReference,
	capability string,
) (ProjectIndexView, error) {
	authorize := func(source ConfigSource) error { return service.authorize(source, ref, capability) }
	snapshot, found, err := service.repository.ReadProjectIndex(ctx, ref, authorize)
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("read project index: %w", err)
	}
	if !found {
		return ProjectIndexView{}, ErrCredential
	}
	if err := authorize(snapshot.Source); err != nil {
		return ProjectIndexView{}, err
	}
	if err := ctx.Err(); err != nil {
		return ProjectIndexView{}, fmt.Errorf("project index cancelled: %w", err)
	}
	return buildProjectIndex(snapshot)
}

func (service *ProjectIndexes) authorize(source ConfigSource, ref ProjectIndexReference, capability string) error {
	if source.State != "ACTIVE" || !validProjectAuthority(service.policy, source, capability) {
		return ErrCredential
	}
	if ref.PreviewOnly && (source.Purpose != "REVIEW_PREVIEW" || source.ContentKind != "ONS_PROJECT") {
		return ErrCredential
	}
	return nil
}

func buildProjectIndex(snapshot ProjectIndexSnapshot) (ProjectIndexView, error) {
	files, root, format, err := projectIndexProjection(snapshot)
	if err != nil {
		return ProjectIndexView{}, err
	}
	policy, err := projectIndexPolicyFor(format, snapshot.Source.DependencyJSON)
	if err != nil {
		return ProjectIndexView{}, err
	}
	if policy.firstIsMarker && len(files) > 0 {
		policy.marker = files[0].Path
	}
	return buildProjectIndexDocument(root, snapshot.Source.Title, policy, files)
}
