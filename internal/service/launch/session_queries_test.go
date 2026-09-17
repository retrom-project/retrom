package launch

import (
	"context"
	"errors"
	"reflect"
	model "retrom/internal/model/launch"
	"testing"
)

type sessionReaderStub struct {
	session    model.SessionRecord
	files      []model.BundleFile
	dimensions model.MultiDiscTelemetryDimensions
	found      bool
	failure    error
	calls      int
	ref        model.SessionRef
}

func (stub *sessionReaderStub) Session(_ context.Context, ref model.SessionRef) (model.SessionRecord, bool, error) {
	stub.calls++
	stub.ref = ref
	return stub.session, stub.found, stub.failure
}

func (stub *sessionReaderStub) SaveSession(context.Context, string) (model.SessionRecord, bool, error) {
	stub.calls++
	return stub.session, stub.found, stub.failure
}

func (stub *sessionReaderStub) Bundle(_ context.Context, ref model.SessionRef, _ string) (model.BundleRecord, bool, error) {
	stub.calls++
	stub.ref = ref
	return model.BundleRecord{Session: stub.session, Files: stub.files}, stub.found, stub.failure
}

func (stub *sessionReaderStub) MultiDisc(context.Context, string) (model.MultiDiscRecord, bool, error) {
	stub.calls++
	return model.MultiDiscRecord{Session: stub.session, Dimensions: stub.dimensions}, stub.found, stub.failure
}

type targetAssetsStub struct {
	paths            []string
	provider, target string
	found            bool
}

func (stub *targetAssetsStub) AssetPaths(provider, target string) ([]string, bool) {
	stub.provider, stub.target = provider, target
	return stub.paths, stub.found
}

func validSessionStub() *sessionReaderStub {
	return &sessionReaderStub{session: activeSession(), found: true}
}

func TestSaveAccessAllowsCreatedAndActiveSessionsOnly(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"CREATED", "ACTIVE", "FINISHED", "EXPIRED", "REVOKED", "unknown"} {
		reader := validSessionStub()
		reader.session.State = state
		reader.session.SaveAccess = "NETPLAY_DISABLED"
		service := NewSessionQueries(reader, nil, queryClock, matchTestCapability)
		actual, err := service.SaveAccess(t.Context(), "launch", "valid")
		if state == "CREATED" || state == "ACTIVE" {
			if err != nil || actual != "NETPLAY_DISABLED" {
				t.Fatalf("state=%s access=%s error=%v", state, actual, err)
			}
		} else if !errors.Is(err, model.ErrCredential) || actual != "" {
			t.Fatalf("state=%s access=%s error=%v", state, actual, err)
		}
	}
}

func TestBundlesValidateRoleAndAuthorityBeforeReturningFiles(t *testing.T) {
	t.Parallel()
	reader := validSessionStub()
	reader.files = []model.BundleFile{{LogicalName: "bios.bin", SHA256: "frozen"}}
	service := NewSessionQueries(reader, nil, queryClock, matchTestCapability)
	if _, err := service.BundleFiles(t.Context(), model.SessionRef{ID: "launch"}, "valid", "DISC"); !errors.Is(err, model.ErrCredential) || reader.calls != 0 {
		t.Fatalf("invalid kind query calls=%d error=%v", reader.calls, err)
	}
	actual, err := service.BundleFiles(t.Context(), model.SessionRef{ID: "preview", Preview: true}, "valid", "BIOS_BUNDLE")
	if err != nil || !reflect.DeepEqual(actual, reader.files) || !reader.ref.Preview {
		t.Fatalf("files=%#v ref=%#v error=%v", actual, reader.ref, err)
	}
	reader.files = nil
	actual, err = service.BundleFiles(t.Context(), model.SessionRef{ID: "launch"}, "valid", "PARENT")
	if err != nil || actual == nil || len(actual) != 0 {
		t.Fatalf("empty bundle=%#v error=%v", actual, err)
	}
	reader.session.HardExpiresAtMS = 100
	actual, err = service.BundleFiles(t.Context(), model.SessionRef{ID: "launch"}, "valid", "PARENT")
	if !errors.Is(err, model.ErrCredential) || actual != nil {
		t.Fatalf("expired bundle=%#v error=%v", actual, err)
	}
}

func TestMultidiscTelemetryUsesFrozenDimensionsAndBounds(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 1, 2, 8, 9} {
		reader := validSessionStub()
		reader.dimensions = model.MultiDiscTelemetryDimensions{PlatformKey: "saturn", TargetKey: "yabause", BundleDigest: "frozen", DiscCount: count}
		service := NewSessionQueries(reader, nil, queryClock, matchTestCapability)
		actual, err := service.MultiDiscTelemetryDimensions(t.Context(), "launch", "valid")
		if count == 2 || count == 8 {
			if err != nil || actual != reader.dimensions {
				t.Fatalf("dimensions=%#v error=%v", actual, err)
			}
		} else if !errors.Is(err, model.ErrCredential) || actual != (model.MultiDiscTelemetryDimensions{}) {
			t.Fatalf("dimensions=%#v error=%v", actual, err)
		}
	}
}

func TestProviderAssetRequiresUniqueDeclaredBasename(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, basename string
		paths          []string
		found, allowed bool
	}{
		{"declared", "runtime.wasm", []string{"assets/runtime.wasm"}, true, true},
		{"unknown", "other.wasm", []string{"assets/runtime.wasm"}, true, false},
		{"ambiguous", "runtime.wasm", []string{"one/runtime.wasm", "two/runtime.wasm"}, true, false},
		{"traversal", "../runtime.wasm", nil, true, false},
		{"missing target", "runtime.wasm", []string{"runtime.wasm"}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := validSessionStub()
			assets := &targetAssetsStub{paths: test.paths, found: test.found}
			service := NewSessionQueries(reader, assets, queryClock, matchTestCapability)
			actual, err := service.ProviderAssetAuthorized(t.Context(), model.SessionRef{ID: "preview", Preview: true}, test.basename)
			if test.allowed {
				if err != nil || actual.Path != "assets/runtime.wasm" || actual.BundleSHA256 != "frozen" || assets.provider != "provider" || assets.target != "target" || !reader.ref.Preview {
					t.Fatalf("asset=%#v assets=%#v ref=%#v error=%v", actual, assets, reader.ref, err)
				}
			} else if !errors.Is(err, model.ErrCredential) || actual != (model.ProviderAsset{}) {
				t.Fatalf("asset=%#v error=%v", actual, err)
			}
		})
	}
}

func TestSessionQueriesPreserveStorageCausesWithoutPartialResults(t *testing.T) {
	t.Parallel()
	reader := validSessionStub()
	reader.failure = context.Canceled
	assets := &targetAssetsStub{found: true, paths: []string{"asset"}}
	service := NewSessionQueries(reader, assets, queryClock, matchTestCapability)
	if result, err := service.SaveAccess(t.Context(), "id", "valid"); result != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("save=%s error=%v", result, err)
	}
	if result, err := service.BundleFiles(t.Context(), model.SessionRef{ID: "id"}, "valid", "PARENT"); result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("bundle=%v error=%v", result, err)
	}
	if result, err := service.MultiDiscTelemetryDimensions(t.Context(), "id", "valid"); result != (model.MultiDiscTelemetryDimensions{}) || !errors.Is(err, context.Canceled) {
		t.Fatalf("telemetry=%v error=%v", result, err)
	}
	if result, err := service.ProviderAssetAuthorized(t.Context(), model.SessionRef{ID: "id"}, "asset"); result != (model.ProviderAsset{}) || !errors.Is(err, context.Canceled) {
		t.Fatalf("asset=%v error=%v", result, err)
	}
}
