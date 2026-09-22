package launch

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type contentReaderStub struct {
	record       ContentRecord
	external     ExternalRecord
	found        bool
	failure      error
	calls        []bool
	projectCalls int
	ref          SessionRef
}

func (stub *contentReaderStub) ProductContent(_ context.Context, _, _ string, folded bool) (ContentRecord, bool, error) {
	stub.calls = append(stub.calls, folded)
	return stub.record, stub.found || folded, stub.failure
}

func (stub *contentReaderStub) PreviewContent(context.Context, string, string) (ContentRecord, bool, error) {
	return stub.record, stub.found, stub.failure
}

func (stub *contentReaderStub) PreviewProject(_ context.Context, _, _ string, folded bool) (ContentRecord, bool, error) {
	stub.projectCalls++
	stub.calls = append(stub.calls, folded)
	return stub.record, stub.found, stub.failure
}

func (stub *contentReaderStub) External(_ context.Context, ref SessionRef, _ string) (ExternalRecord, bool, error) {
	stub.ref = ref
	return stub.external, stub.found, stub.failure
}
func queryClock() time.Time { return time.UnixMilli(100) }
func matchTestCapability(capability string, hash []byte) bool {
	return capability == "valid" && string(hash) == "hash"
}

func activeSession() SessionRecord {
	return SessionRecord{
		State: "ACTIVE", HardExpiresAtMS: 101, CredentialHash: []byte("hash"),
		ProviderID: "provider", TargetID: "target", BundleSHA256: "frozen",
	}
}

func validContentStub() *contentReaderStub {
	session := activeSession()
	return &contentReaderStub{
		found: true, record: ContentRecord{Session: session, Content: ContentView{
			Digest: "digest", Format: "RPG_MAKER_PROJECT", ProviderID: "provider", TargetID: "target", BundleSHA256: "frozen",
		}},
		external: ExternalRecord{Session: session, Content: ExternalView{Digest: "bios", Kind: "BIOS"}},
	}
}

func TestContentAccessRequiresActiveUnexpiredCapability(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, state, capability string
		expires                 int64
		found                   bool
		allowed                 bool
	}{
		{"active", "ACTIVE", "valid", 101, true, true},
		{"created", "CREATED", "valid", 101, true, false},
		{"finished", "FINISHED", "valid", 101, true, false},
		{"expired", "EXPIRED", "valid", 101, true, false},
		{"revoked", "REVOKED", "valid", 101, true, false},
		{"deadline", "ACTIVE", "valid", 100, true, false},
		{"bad capability", "ACTIVE", "wrong", 101, true, false},
		{"missing", "ACTIVE", "valid", 101, false, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			reader := validContentStub()
			reader.found, reader.record.Session.State, reader.record.Session.HardExpiresAtMS = test.found, test.state, test.expires
			service := NewContentAccess(reader, queryClock, matchTestCapability)
			actual, err := service.Content(t.Context(), "launch", test.capability, "file")
			if test.allowed {
				if err != nil || !reflect.DeepEqual(actual, reader.record.Content) {
					t.Fatalf("content=%#v error=%v", actual, err)
				}
			} else if !errors.Is(err, ErrCredential) || actual != (ContentView{}) {
				t.Fatalf("unauthorized content=%#v error=%v", actual, err)
			}
		})
	}
}

func TestRPGFallbackOnlyFollowsMissingExactPath(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		found   bool
		failure error
		calls   []bool
	}{
		{"exact", true, nil, []bool{false}},
		{"folded", false, nil, []bool{false, true}},
		{"cancelled", false, context.Canceled, []bool{false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := validContentStub()
			reader.found, reader.failure = test.found, test.failure
			service := NewContentAccess(reader, queryClock, matchTestCapability)
			actual, err := service.RPGProjectContentAuthorized(t.Context(), "launch", "RPG_RT.ldb", false)
			if !reflect.DeepEqual(reader.calls, test.calls) {
				t.Fatalf("calls=%v", reader.calls)
			}
			if test.failure != nil {
				if !errors.Is(err, test.failure) || actual != (ContentView{}) {
					t.Fatalf("content=%#v error=%v", actual, err)
				}
			} else if err != nil || actual.Digest != "digest" {
				t.Fatalf("content=%#v error=%v", actual, err)
			}
		})
	}
}

func TestPreviewProjectPathsAndFormatsRemainConstrained(t *testing.T) {
	t.Parallel()
	for _, logicalName := range []string{"../file", "/file", "a/../file", "a\\file", ""} {
		reader := validContentStub()
		service := NewContentAccess(reader, queryClock, matchTestCapability)
		if _, err := service.PreviewProjectContent(t.Context(), "preview", "valid", logicalName); !errors.Is(err, ErrCredential) || reader.projectCalls != 0 {
			t.Fatalf("path=%q calls=%d error=%v", logicalName, reader.projectCalls, err)
		}
	}
	for _, format := range []string{"RPG_MAKER_PROJECT", "NXENGINE_PROJECT", "RETROM_SINGLE_FILE_V1"} {
		reader := validContentStub()
		reader.record.Content.Format = format
		service := NewContentAccess(reader, queryClock, matchTestCapability)
		actual, err := service.PreviewProjectContent(t.Context(), "preview", "valid", "assets/file.png")
		if format == "RETROM_SINGLE_FILE_V1" {
			if !errors.Is(err, ErrCredential) {
				t.Fatalf("nonproject allowed: %v", err)
			}
		} else if err != nil || actual.Format != format {
			t.Fatalf("format=%q content=%#v error=%v", format, actual, err)
		}
	}
}

func TestIsolatedAndPreviewAccessKeepDistinctAuthority(t *testing.T) {
	t.Parallel()
	reader := validContentStub()
	service := NewContentAccess(reader, queryClock, matchTestCapability)
	if _, err := service.ContentAuthorized(t.Context(), "preview", "RPG_RT.ldb", true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reader.calls, []bool{false}) {
		t.Fatalf("generic preview folded paths: %v", reader.calls)
	}
	if _, err := service.RPGProjectContentAuthorized(t.Context(), "preview", "rpg_rt.LDB", true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reader.calls, []bool{false, true}) {
		t.Fatalf("RPG preview calls=%v", reader.calls)
	}
	if _, err := service.PreviewContent(t.Context(), "preview", "invalid", "file"); !errors.Is(err, ErrCredential) {
		t.Fatalf("preview credential=%v", err)
	}
	external, err := service.External(t.Context(), SessionRef{ID: "preview", Preview: true}, "valid", "bios.bin")
	if err != nil || external.Digest != "bios" || !reader.ref.Preview {
		t.Fatalf("external=%#v ref=%#v error=%v", external, reader.ref, err)
	}
	reader.record.Session.HardExpiresAtMS = 100
	if _, err := service.ContentAuthorized(t.Context(), "preview", "RPG_RT.ldb", true); !errors.Is(err, ErrCredential) {
		t.Fatalf("expired isolated content=%v", err)
	}
}

func TestTyranoContentKeepsCausesAndRejectsOtherFormats(t *testing.T) {
	for _, preview := range []bool{false, true} {
		reader := validContentStub()
		service := NewContentAccess(reader, queryClock, matchTestCapability)
		for _, cause := range []error{context.Canceled, errors.New("read failure")} {
			reader.failure = cause
			if _, err := service.TyranoScriptProjectContentAuthorized(t.Context(), "id", "index.html", preview); !errors.Is(err, cause) {
				t.Fatalf("preview=%t cause lost: %v", preview, err)
			}
		}
		reader.failure = nil
		if _, err := service.TyranoScriptProjectContentAuthorized(t.Context(), "id", "index.html", preview); !errors.Is(err, ErrCredential) {
			t.Fatalf("wrong format preview=%t: %v", preview, err)
		}
		reader.record.Content.Format = "TYRANOSCRIPT_PROJECT"
		if _, err := service.TyranoScriptProjectContentAuthorized(t.Context(), "id", "index.html", preview); err != nil {
			t.Fatalf("valid format preview=%t: %v", preview, err)
		}
		for _, folded := range reader.calls {
			if folded {
				t.Fatal("Tyrano content must preserve exact path matching")
			}
		}
	}
}
