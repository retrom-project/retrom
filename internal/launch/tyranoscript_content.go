package launch

import (
	"context"
)

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
