package launch

import (
	"context"
	"fmt"

	application "retrom/internal/service/launch"
)

type (
	ContentView  = application.ContentView
	ExternalView = application.ExternalView
)

func (service *Service) ContentBlob(ctx context.Context, launchID, capability, logicalName string) (string, error) {
	content, err := service.Content(ctx, launchID, capability, logicalName)
	return content.Digest, err
}

func (service *Service) Content(ctx context.Context, launchID, capability, logicalName string) (ContentView, error) {
	result, err := service.contentAccess().Content(ctx, launchID, capability, logicalName)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) ContentAuthorized(
	ctx context.Context,
	launchID, logicalName string,
	preview bool,
) (ContentView, error) {
	result, err := service.contentAccess().ContentAuthorized(ctx, launchID, logicalName, preview)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) RPGProjectContentAuthorized(
	ctx context.Context,
	launchID, logicalName string,
	preview bool,
) (ContentView, error) {
	result, err := service.contentAccess().RPGProjectContentAuthorized(ctx, launchID, logicalName, preview)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) External(ctx context.Context, launchID, capability, logicalName string) (ExternalView, error) {
	result, err := service.contentAccess().External(ctx, application.SessionRef{ID: launchID}, capability, logicalName)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) ExternalBlob(ctx context.Context, launchID, capability, logicalName string) (string, error) {
	view, err := service.External(ctx, launchID, capability, logicalName)
	return view.Digest, err
}

type (
	Interval   = application.Interval
	PlayEvent  = application.PlayEvent
	PlayResult = application.PlayResult
)
