package httpapi

import "net/http"

const immutablePrivateContent = "private, max-age=31536000, immutable, no-transform"

type runtimeContentGrant struct{ LaunchID, Capability string }

func runtimeContentGrants(request *http.Request) ([]runtimeContentGrant, bool) {
	grants, ok := request.Context().Value(runtimeGrantsKey{}).([]runtimeContentGrant)
	return grants, ok && len(grants) > 0
}
