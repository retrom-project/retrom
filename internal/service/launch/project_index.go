package launch

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/launch"
)

type ProjectIndexes struct {
	repository model.ProjectIndexReader
	policy     accessPolicy
}

func NewProjectIndexes(repository model.ProjectIndexReader, now func() time.Time, matches model.MatchCapability) *ProjectIndexes {
	return &ProjectIndexes{repository: repository, policy: accessPolicy{now: now, matches: matches}}
}

func (service *ProjectIndexes) Index(
	ctx context.Context,
	ref model.ProjectIndexReference,
	capability string,
) (model.ProjectIndexView, error) {
	authorize := func(source model.ConfigSource) error { return service.authorize(source, ref, capability) }
	snapshot, found, err := service.repository.ReadProjectIndex(ctx, ref, authorize)
	if err != nil {
		return model.ProjectIndexView{}, fmt.Errorf("read project index: %w", err)
	}
	if !found {
		return model.ProjectIndexView{}, model.ErrCredential
	}
	if err := authorize(snapshot.Source); err != nil {
		return model.ProjectIndexView{}, err
	}
	if err := ctx.Err(); err != nil {
		return model.ProjectIndexView{}, fmt.Errorf("project index cancelled: %w", err)
	}
	return buildProjectIndex(snapshot)
}

func (service *ProjectIndexes) authorize(source model.ConfigSource, ref model.ProjectIndexReference, capability string) error {
	if source.State != "ACTIVE" || !validProjectAuthority(service.policy, source, capability) {
		return model.ErrCredential
	}
	if ref.PreviewOnly && (source.Purpose != "REVIEW_PREVIEW" || source.ContentKind != "ONS_PROJECT") {
		return model.ErrCredential
	}
	return nil
}

func buildProjectIndex(snapshot model.ProjectIndexSnapshot) (model.ProjectIndexView, error) {
	files, root, format, err := projectIndexProjection(snapshot)
	if err != nil {
		return model.ProjectIndexView{}, err
	}
	policy, err := projectIndexPolicyFor(format, snapshot.Source.DependencyJSON)
	if err != nil {
		return model.ProjectIndexView{}, err
	}
	if policy.firstIsMarker && len(files) > 0 {
		policy.marker = files[0].Path
	}
	return buildProjectIndexDocument(root, snapshot.Source.Title, policy, files)
}
