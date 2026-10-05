package httpapi

import (
	"testing"

	"retrom/internal/httpapi/generated"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestEveryRequiredIdempotentOperationHasAnAtomicOwner(t *testing.T) {
	spec, err := generated.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	domainOwned := map[string]bool{
		"deleteAdminAccountLink": true, "deleteAdminUser": true, "patchAdminUser": true,
		"postAdminInvitation": true, "postAdminUserPasswordResetLink": true, "postLaunch": true,
		"postLocalGameSave": true, "postRuntimeSaveState": true, "postAdminGameContentReplacement": true,
		"postAdminPlatformInstanceRecommendationsApply": true, "postAdminPlatformInstance": true,
		"postFavoriteOrganize": true, "postFavoriteUnfavorite": true, "postFavoriteRestore": true,
		"postFavoriteFolder": true, "patchFavoriteFolder": true, "deleteFavoriteFolder": true, "deleteAdminGame": true,
	}
	seen := map[string]bool{}
	for _, path := range spec.Paths.Map() {
		for _, operation := range path.Operations() {
			if !requiresCommandKey(append(path.Parameters, operation.Parameters...)) {
				continue
			}
			id := lowerFirst(operation.OperationID)
			_, commandOwned := commandResponses[id]
			if commandOwned == domainOwned[id] {
				t.Errorf("operation %s must have exactly one atomic owner", id)
			}
			seen[id] = true
		}
	}
	for id := range commandResponses {
		if !seen[id] {
			t.Errorf("command %s has no required OpenAPI idempotency contract", id)
		}
	}
	for id := range domainOwned {
		if !seen[id] {
			t.Errorf("domain operation %s has no required OpenAPI idempotency contract", id)
		}
	}
	if len(commandResponses) != 39 || len(seen) != 57 {
		t.Fatalf("coverage changed: commands=%d, all=%d", len(commandResponses), len(seen))
	}
}

func requiresCommandKey(parameters openapi3.Parameters) bool {
	for _, parameter := range parameters {
		if parameter.Value.Name == "Idempotency-Key" && parameter.Value.Required {
			return true
		}
	}
	return false
}
