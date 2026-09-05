package launch

import (
	"context"

	"retrom/internal/tyranoscript/detector"
)

const maximumTyranoScriptProjectFiles = 10_000

func (service *Service) reviewPreviewTyranoScriptContent(
	ctx context.Context,
	source reviewPreviewSource,
) (reviewPreviewContentSet, error) {
	profile, err := detector.ParseSnapshot(source.DependencySnapshot)
	if err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	return service.reviewPreviewProjectContent(
		ctx, source, profile.EntryPath, tyranoScriptProjectFormat, maximumTyranoScriptProjectFiles,
		"TyranoScript",
	)
}

func (service *Service) TyranoScriptProjectContentAuthorized(
	ctx context.Context,
	sessionID, logicalName string,
	preview bool,
) (ContentView, error) {
	content, err := service.ContentAuthorized(ctx, sessionID, logicalName, preview)
	if err != nil || content.Format != tyranoScriptProjectFormat {
		return ContentView{}, ErrCredential
	}
	return content, nil
}
