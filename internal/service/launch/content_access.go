package launch

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/launch"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
)

type ContentAccess struct {
	reader model.ContentReader
	policy accessPolicy
}

func NewContentAccess(reader model.ContentReader, now func() time.Time, matches model.MatchCapability) *ContentAccess {
	return &ContentAccess{reader: reader, policy: accessPolicy{now: now, matches: matches}}
}

func (service *ContentAccess) Content(ctx context.Context, id, capability, logicalName string) (model.ContentView, error) {
	record, found, err := service.reader.ProductContent(ctx, id, logicalName, false)
	if err != nil {
		return model.ContentView{}, fmt.Errorf("read launch content: %w", err)
	}
	if !found || !service.policy.authorized(record.Session, capability) {
		return model.ContentView{}, model.ErrCredential
	}
	return record.Content, nil
}

// ContentAuthorized is used after authenticating an isolated runtime credential.
func (service *ContentAccess) ContentAuthorized(
	ctx context.Context,
	id, logicalName string,
	preview bool,
) (model.ContentView, error) {
	if preview {
		return service.project(ctx, id, logicalName, false, "", false)
	}
	record, found, err := service.reader.ProductContent(ctx, id, logicalName, false)
	if err != nil {
		return model.ContentView{}, fmt.Errorf("read authorized launch content: %w", err)
	}
	if !found || !service.policy.active(record.Session) {
		return model.ContentView{}, model.ErrCredential
	}
	return record.Content, nil
}

func (service *ContentAccess) RPGProjectContentAuthorized(
	ctx context.Context,
	id, logicalName string,
	preview bool,
) (model.ContentView, error) {
	if preview {
		content, err := service.project(ctx, id, logicalName, true, "", false)
		if err != nil {
			return model.ContentView{}, err
		}
		if content.Format != "RPG_MAKER_PROJECT" {
			return model.ContentView{}, model.ErrCredential
		}
		return content, nil
	}
	record, found, err := service.reader.ProductContent(ctx, id, logicalName, false)
	if err == nil && !found {
		record, found, err = service.reader.ProductContent(ctx, id, logicalName, true)
	}
	if err != nil {
		return model.ContentView{}, fmt.Errorf("read RPG project content: %w", err)
	}
	if !found || !service.policy.active(record.Session) || record.Content.Format != "RPG_MAKER_PROJECT" {
		return model.ContentView{}, model.ErrCredential
	}
	return record.Content, nil
}

func (service *ContentAccess) PreviewContent(
	ctx context.Context,
	id, capability, logicalName string,
) (model.ContentView, error) {
	record, found, err := service.reader.PreviewContent(ctx, id, logicalName)
	if err != nil {
		return model.ContentView{}, fmt.Errorf("read preview content: %w", err)
	}
	if !found || !service.policy.authorized(record.Session, capability) {
		return model.ContentView{}, model.ErrCredential
	}
	return record.Content, nil
}

func (service *ContentAccess) PreviewProjectContent(
	ctx context.Context,
	id, capability, logicalName string,
) (model.ContentView, error) {
	return service.project(ctx, id, logicalName, true, capability, true)
}

func (service *ContentAccess) project(
	ctx context.Context,
	id, logicalName string,
	folded bool,
	capability string,
	checkCapability bool,
) (model.ContentView, error) {
	normalized, err := importing.ValidateLogicalPath(logicalName)
	if err != nil || normalized != logicalName {
		return model.ContentView{}, model.ErrCredential
	}
	record, found, err := service.reader.PreviewProject(ctx, id, logicalName, folded)
	if err != nil {
		return model.ContentView{}, fmt.Errorf("read preview project content: %w", err)
	}
	if !found || !service.policy.active(record.Session) ||
		!contentprofile.IsProjectContentKind(contentprofile.ContentKind(record.Content.Format)) {
		return model.ContentView{}, model.ErrCredential
	}
	if checkCapability && !service.policy.authorized(record.Session, capability) {
		return model.ContentView{}, model.ErrCredential
	}
	return record.Content, nil
}

func (service *ContentAccess) External(
	ctx context.Context,
	ref model.SessionRef,
	capability, logicalName string,
) (model.ExternalView, error) {
	record, found, err := service.reader.External(ctx, ref, logicalName)
	if err != nil {
		return model.ExternalView{}, fmt.Errorf("read launch external content: %w", err)
	}
	if !found || !service.policy.authorized(record.Session, capability) {
		return model.ExternalView{}, model.ErrCredential
	}
	return record.Content, nil
}

func (service *ContentAccess) TyranoScriptProjectContentAuthorized(
	ctx context.Context,
	id, logicalName string,
	preview bool,
) (model.ContentView, error) {
	content, err := service.ContentAuthorized(ctx, id, logicalName, preview)
	if err != nil {
		return model.ContentView{}, err
	}
	if content.Format != "TYRANOSCRIPT_PROJECT" {
		return model.ContentView{}, model.ErrCredential
	}
	return content, nil
}
