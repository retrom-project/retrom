package launch

import (
	"context"
	"fmt"
	"io"
	model "retrom/internal/model/launch"
)

func (service *Service) ContentBlob(ctx context.Context, launchID, capability, logicalName string) (string, error) {
	content, err := service.Content(ctx, launchID, capability, logicalName)
	return content.Digest, err
}

func (service *Service) Content(ctx context.Context, launchID, capability, logicalName string) (model.ContentView, error) {
	result, err := service.dependencies.Content.Content(ctx, launchID, capability, logicalName)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) ContentAuthorized(
	ctx context.Context,
	launchID, logicalName string,
	preview bool,
) (model.ContentView, error) {
	result, err := service.dependencies.Content.ContentAuthorized(ctx, launchID, logicalName, preview)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) RPGProjectContentAuthorized(
	ctx context.Context,
	launchID, logicalName string,
	preview bool,
) (model.ContentView, error) {
	result, err := service.dependencies.Content.RPGProjectContentAuthorized(ctx, launchID, logicalName, preview)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) External(ctx context.Context, launchID, capability, logicalName string) (model.ExternalView, error) {
	result, err := service.dependencies.Content.External(ctx, model.SessionRef{ID: launchID}, capability, logicalName)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) ExternalBlob(ctx context.Context, launchID, capability, logicalName string) (string, error) {
	view, err := service.External(ctx, launchID, capability, logicalName)
	return view.Digest, err
}

func (service *Service) Config(ctx context.Context, id, capability string) (Config, error) {
	configuration, err := service.dependencies.Config.Issue(ctx, model.SessionRef{ID: id}, capability)
	if err != nil {
		return Config{}, fmt.Errorf("launch config: %w", err)
	}
	return configuration, nil
}

func (service *Service) MultiDiscTelemetryDimensions(
	ctx context.Context,
	launchID, capability string,
) (model.MultiDiscTelemetryDimensions, error) {
	result, err := service.dependencies.Sessions.MultiDiscTelemetryDimensions(ctx, launchID, capability)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) BundleFiles(ctx context.Context, launchID, capability, kind string) ([]model.BundleFile, error) {
	result, err := service.dependencies.Sessions.BundleFiles(ctx, model.SessionRef{ID: launchID}, capability, kind)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) ReviewPreviewContent(
	ctx context.Context,
	previewID, capability, logicalName string,
) (model.ContentView, error) {
	result, err := service.dependencies.Content.PreviewContent(ctx, previewID, capability, logicalName)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) ReviewPreviewExternal(
	ctx context.Context,
	previewID, capability, logicalName string,
) (model.ExternalView, error) {
	result, err := service.dependencies.Content.External(
		ctx,
		model.SessionRef{ID: previewID, Preview: true},
		capability,
		logicalName,
	)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) ReviewPreviewBundleFiles(
	ctx context.Context,
	previewID, capability, kind string,
) ([]model.BundleFile, error) {
	result, err := service.dependencies.Sessions.BundleFiles(
		ctx,
		model.SessionRef{ID: previewID, Preview: true},
		capability,
		kind,
	)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) ProjectIndex(ctx context.Context, id, capability string) (model.ProjectIndexView, error) {
	result, err := service.dependencies.Indexes.Index(ctx, model.ProjectIndexReference{ID: id}, capability)
	if err != nil {
		return result, fmt.Errorf("launch project index: %w", err)
	}
	return result, nil
}

func (service *Service) ReviewPreviewProjectIndex(
	ctx context.Context,
	id, capability string,
) (model.ProjectIndexView, error) {
	result, err := service.dependencies.Indexes.Index(
		ctx,
		model.ProjectIndexReference{ID: id, PreviewOnly: true},
		capability,
	)
	if err != nil {
		return result, fmt.Errorf("review project index: %w", err)
	}
	return result, nil
}

func (service *Service) ReviewPreviewProjectContent(
	ctx context.Context,
	previewID, capability, logicalName string,
) (model.ContentView, error) {
	result, err := service.dependencies.Content.PreviewProjectContent(ctx, previewID, capability, logicalName)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) ProjectContentIdentity(ctx context.Context, id, capability string) (string, error) {
	queries := service.dependencies.Projects
	identity, err := queries.Identity(ctx, id, capability)
	if err != nil {
		return "", fmt.Errorf("launch project identity: %w", err)
	}
	return identity, nil
}

func (service *Service) ProjectContentRoot(ctx context.Context, id, capability string) (string, error) {
	identity, err := service.ProjectContentIdentity(ctx, id, capability)
	if err != nil {
		return "", err
	}
	return RuntimeProjectContentRoot(identity)
}

func (service *Service) Create(ctx context.Context, profileID string, request model.CreateRequest) (model.Created, error) {
	result, err := service.dependencies.Product.Create(
		ctx,
		model.ProductCreateCommand{ProfileID: profileID, Request: request},
	)
	if err != nil {
		return model.Created{}, fmt.Errorf("launch product creation: %w", err)
	}
	return result.Created, nil
}

func (service *Service) CreateProduct(
	ctx context.Context,
	command model.ProductCreateCommand,
) (model.ProductReceipt, error) {
	result, err := service.dependencies.Product.Create(ctx, command)
	if err != nil {
		return model.ProductReceipt{}, fmt.Errorf("launch product receipt: %w", err)
	}
	return result, nil
}

func (service *Service) CreateReviewPreview(
	ctx context.Context,
	request model.ReviewPreviewRequest,
) (model.ReviewPreviewCreated, error) {
	result, err := service.dependencies.Preview.Create(ctx, request)
	if err != nil {
		return model.ReviewPreviewCreated{}, fmt.Errorf("review preview creation: %w", err)
	}
	return result, nil
}

func (service *Service) CreateNetplay(ctx context.Context, request model.NetplayCreateRequest) (model.Created, error) {
	result, err := service.dependencies.Netplay.CreateNetplay(ctx, request)
	if err != nil {
		return model.Created{}, fmt.Errorf("launch netplay creation: %w", err)
	}
	return result, nil
}

func (service *Service) ReviewPreviewConfig(ctx context.Context, id, capability string) (Config, error) {
	configuration, err := service.dependencies.Config.Issue(ctx, model.SessionRef{ID: id, Preview: true}, capability)
	if err != nil {
		return Config{}, fmt.Errorf("review config: %w", err)
	}
	return configuration, nil
}

func (service *Service) StoreReviewScreenshot(
	ctx context.Context,
	previewID, capability string,
	reader io.Reader,
) (model.ReviewScreenshot, error) {
	result, err := service.dependencies.Screenshots.Store(
		ctx, previewID, capability, reader,
	)
	if err != nil {
		return model.ReviewScreenshot{}, fmt.Errorf("store review screenshot: %w", err)
	}
	return result, nil
}

func (service *Service) RecordPlay(
	ctx context.Context,
	launchID, capability, kind string,
	event model.PlayEvent,
) (model.PlayResult, error) {
	controller := service.dependencies.Play
	result, err := controller.RecordPlay(ctx, launchID, capability, kind, event)
	if err != nil {
		return model.PlayResult{}, fmt.Errorf("launch play: %w", err)
	}
	return result, nil
}

func (service *Service) ProviderAssetAuthorized(
	ctx context.Context,
	sessionID string,
	preview bool,
	basename string,
) (model.ProviderAsset, error) {
	result, err := service.dependencies.Sessions.ProviderAssetAuthorized(
		ctx,
		model.SessionRef{ID: sessionID, Preview: preview},
		basename,
	)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) SaveAccess(ctx context.Context, launchID, capability string) (string, error) {
	result, err := service.dependencies.Sessions.SaveAccess(ctx, launchID, capability)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) TyranoScriptProjectContentAuthorized(
	ctx context.Context,
	id, logicalName string,
	preview bool,
) (model.ContentView, error) {
	return service.dependencies.Content.TyranoScriptProjectContentAuthorized(ctx, id, logicalName, preview)
}
