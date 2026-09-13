package launch

import "context"

func (service *Service) TyranoScriptProjectContentAuthorized(ctx context.Context, id, logicalName string, preview bool) (ContentView, error) {
	return service.contentAccess().TyranoScriptProjectContentAuthorized(ctx, id, logicalName, preview)
}
