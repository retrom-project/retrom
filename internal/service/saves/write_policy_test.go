package saves

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/capability/runtime/runtimebundle"
)

func writableLaunch() Launch {
	return Launch{
		PrincipalID: "user", ProfileID: "profile", Purpose: "PRODUCT", GameID: "game",
		State: "ACTIVE", HardExpiresAtMS: 200, GameStatus: "PUBLISHED", CredentialHash: []byte("credential"),
		Checkpoint:         runtimebundle.Checkpoint{WriteFormat: "opaque-v1", MaxBytes: 100, Semantics: "GAME_SAVE"},
		HasGameSaveBinding: true,
	}
}

func TestCheckpointWriteRevalidatesCurrentAuthority(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Launch)
		want   error
	}{
		{"active", func(*Launch) {}, nil},
		{"expired", func(value *Launch) { value.HardExpiresAtMS = 100 }, ErrCredential},
		{"revoked", func(value *Launch) { value.State = "REVOKED" }, ErrCredential},
		{"deleted game", func(value *Launch) { value.GameStatus = "DELETED" }, ErrCredential},
		{"owner changed", func(value *Launch) { value.PrincipalID = "other" }, ErrCredential},
		{"profile changed", func(value *Launch) { value.ProfileID = "other" }, ErrCredential},
		{"credential rotated", func(value *Launch) { value.CredentialHash = []byte("rotated") }, ErrCredential},
		{"format changed", func(value *Launch) { value.Checkpoint.WriteFormat = "opaque-v2" }, ErrCredential},
		{"maximum reduced", func(value *Launch) { value.Checkpoint.MaxBytes = 9 }, ErrTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			expected, current := writableLaunch(), writableLaunch()
			test.change(&current)
			service := New(nil, nil, func() time.Time { return time.UnixMilli(100) })
			err := service.ensureWritable(t.Context(), policyLaunchReader{value: current}, "launch", expected, 10)
			if !errors.Is(err, test.want) {
				t.Fatalf("write decision=%v want=%v", err, test.want)
			}
		})
	}
}

func TestLocalDraftAllowsFinishedOwnedGameSaveOnly(t *testing.T) {
	for _, test := range []struct {
		state, semantics string
		binding, allowed bool
	}{
		{"ACTIVE", "GAME_SAVE", true, true},
		{"FINISHED", "GAME_SAVE", true, true},
		{"EXPIRED", "GAME_SAVE", true, true},
		{"REVOKED", "GAME_SAVE", true, false},
		{"FINISHED", "GAME_SAVE", false, false},
		{"FINISHED", "INSTANT_STATE", true, false},
	} {
		value := writableLaunch()
		value.State = test.state
		value.Checkpoint.Semantics = test.semantics
		value.HasGameSaveBinding = test.binding
		if localDraftWritable(value) != test.allowed {
			t.Fatalf("local draft decision for %+v", test)
		}
	}
}

func TestRestoreUsesProviderFormatsAndSizeBounds(t *testing.T) {
	for _, test := range []struct {
		format string
		size   int64
		valid  bool
	}{
		{"opaque-v1", 1, true},
		{"opaque-v1", 100, true},
		{"opaque-v1", 0, false},
		{"opaque-v1", 101, false},
		{"opaque-v0", 50, false},
	} {
		restore := Restore{Format: test.format, Size: test.size, Checkpoint: runtimebundle.Checkpoint{
			MaxBytes: 100, ReadFormats: []string{"opaque-v1"},
		}}
		if validRestore(restore) != test.valid {
			t.Fatalf("restore decision for %+v", test)
		}
	}
}

func TestCheckpointReadErrorsRetainCause(t *testing.T) {
	failure := errors.New("storage unavailable")
	service := New(policyRepository{err: failure}, nil, time.Now)
	_, err := service.loadLaunch(t.Context(), "launch")
	if !errors.Is(err, failure) {
		t.Fatalf("load lost cause: %v", err)
	}
	err = service.ensureWritable(t.Context(), policyLaunchReader{err: failure}, "launch", writableLaunch(), 1)
	if !errors.Is(err, failure) {
		t.Fatalf("write check lost cause: %v", err)
	}
}

type policyLaunchReader struct {
	value Launch
	err   error
}

func (reader policyLaunchReader) LoadLaunch(context.Context, string) (Launch, error) {
	return reader.value, reader.err
}

type policyRepository struct {
	Repository
	err error
}

func (repository policyRepository) LoadLaunch(context.Context, string) (Launch, error) {
	return Launch{}, repository.err
}
