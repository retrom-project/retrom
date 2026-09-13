package launch

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
)

type ContentAccess struct {
	reader ContentReader
	policy accessPolicy
}

func NewContentAccess(reader ContentReader, now func() time.Time, matches MatchCapability) *ContentAccess {
	return &ContentAccess{reader: reader, policy: accessPolicy{now: now, matches: matches}}
}

func (service *ContentAccess) Content(ctx context.Context, id, capability, logicalName string) (ContentView, error) {
	record, found, err := service.reader.ProductContent(ctx, id, logicalName, false)
	if err != nil {
		return ContentView{}, fmt.Errorf("read launch content: %w", err)
	}
	if !found || !service.policy.authorized(record.Session, capability) {
		return ContentView{}, ErrCredential
	}
	return record.Content, nil
}

// ContentAuthorized is used after authenticating an isolated runtime credential.
func (service *ContentAccess) ContentAuthorized(
	ctx context.Context,
	id, logicalName string,
	preview bool,
) (ContentView, error) {
	if preview {
		return service.project(ctx, id, logicalName, false, "", false)
	}
	record, found, err := service.reader.ProductContent(ctx, id, logicalName, false)
	if err != nil {
		return ContentView{}, fmt.Errorf("read authorized launch content: %w", err)
	}
	if !found || !service.policy.active(record.Session) {
		return ContentView{}, ErrCredential
	}
	return record.Content, nil
}

func (service *ContentAccess) RPGProjectContentAuthorized(
	ctx context.Context,
	id, logicalName string,
	preview bool,
) (ContentView, error) {
	if preview {
		content, err := service.project(ctx, id, logicalName, true, "", false)
		if err != nil {
			return ContentView{}, err
		}
		if content.Format != "RPG_MAKER_PROJECT" {
			return ContentView{}, ErrCredential
		}
		return content, nil
	}
	record, found, err := service.reader.ProductContent(ctx, id, logicalName, false)
	if err == nil && !found {
		record, found, err = service.reader.ProductContent(ctx, id, logicalName, true)
	}
	if err != nil {
		return ContentView{}, fmt.Errorf("read RPG project content: %w", err)
	}
	if !found || !service.policy.active(record.Session) || record.Content.Format != "RPG_MAKER_PROJECT" {
		return ContentView{}, ErrCredential
	}
	return record.Content, nil
}

func (service *ContentAccess) PreviewContent(
	ctx context.Context,
	id, capability, logicalName string,
) (ContentView, error) {
	record, found, err := service.reader.PreviewContent(ctx, id, logicalName)
	if err != nil {
		return ContentView{}, fmt.Errorf("read preview content: %w", err)
	}
	if !found || !service.policy.authorized(record.Session, capability) {
		return ContentView{}, ErrCredential
	}
	return record.Content, nil
}

func (service *ContentAccess) PreviewProjectContent(
	ctx context.Context,
	id, capability, logicalName string,
) (ContentView, error) {
	return service.project(ctx, id, logicalName, true, capability, true)
}

func (service *ContentAccess) project(
	ctx context.Context,
	id, logicalName string,
	folded bool,
	capability string,
	checkCapability bool,
) (ContentView, error) {
	normalized, err := importing.ValidateLogicalPath(logicalName)
	if err != nil || normalized != logicalName {
		return ContentView{}, ErrCredential
	}
	record, found, err := service.reader.PreviewProject(ctx, id, logicalName, folded)
	if err != nil {
		return ContentView{}, fmt.Errorf("read preview project content: %w", err)
	}
	if !found || !service.policy.active(record.Session) ||
		!contentprofile.IsProjectContentKind(contentprofile.ContentKind(record.Content.Format)) {
		return ContentView{}, ErrCredential
	}
	if checkCapability && !service.policy.authorized(record.Session, capability) {
		return ContentView{}, ErrCredential
	}
	return record.Content, nil
}

func (service *ContentAccess) External(
	ctx context.Context,
	ref SessionRef,
	capability, logicalName string,
) (ExternalView, error) {
	record, found, err := service.reader.External(ctx, ref, logicalName)
	if err != nil {
		return ExternalView{}, fmt.Errorf("read launch external content: %w", err)
	}
	if !found || !service.policy.authorized(record.Session, capability) {
		return ExternalView{}, ErrCredential
	}
	return record.Content, nil
}

func (service *ContentAccess) TyranoScriptProjectContentAuthorized(
	ctx context.Context,
	id, logicalName string,
	preview bool,
) (ContentView, error) {
	content, err := service.ContentAuthorized(ctx, id, logicalName, preview)
	if err != nil {
		return ContentView{}, err
	}
	if content.Format != "TYRANOSCRIPT_PROJECT" {
		return ContentView{}, ErrCredential
	}
	return content, nil
}
