//go:build integration

package launch

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func productCreationFixture(t *testing.T) (reviewCheckpointFixture, CreateRequest) {
	t.Helper()
	fixture, created := newProductPlayFixture(t, false)
	var gameID string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT game_id FROM launch_sessions WHERE id=?`, created.LaunchID).Scan(&gameID); err != nil {
		t.Fatal(err)
	}
	return fixture, CreateRequest{GameID: gameID, ReturnTo: "/games/" + gameID}
}

func TestProductValidationIdentityRetainsEntropyFailure(t *testing.T) {
	fixture := newScummVMFixture(t, []string{"One"})
	published, err := fixture.importer.Approve(t.Context(), fixture.itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	request := CreateRequest{GameID: published.GameID, ReturnTo: "/games/" + published.GameID, ClientCapabilities: Capabilities{SecureContext: true}}
	mustRPGLaunchSQL(t, fixture.database, `UPDATE game_variants SET status='BLOCKED',compatibility_code='VALIDATION_PENDING' WHERE game_id=?`, request.GameID)
	before := productValidationRows(t, reviewCheckpointFixture{database: fixture.database})
	cause := errors.New("product validation entropy unavailable")
	uuid.SetRand(playEntropyFailure{cause: cause})
	defer uuid.SetRand(nil)
	result, err := fixture.service.Create(t.Context(), "scummvm-profile", request)
	if !errors.Is(err, cause) || result.LaunchID != "" || result.JobID != "" {
		t.Fatalf("identity failure: launch=%q job=%q error=%v", result.LaunchID, result.JobID, err)
	}
	if actual := productValidationRows(t, reviewCheckpointFixture{database: fixture.database}); !reflect.DeepEqual(before, actual) {
		t.Fatal("entropy failure left job, input, event or variant writes")
	}
}
