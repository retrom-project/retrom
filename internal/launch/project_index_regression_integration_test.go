//go:build integration

package launch

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"retrom/internal/testsupport"
)

func activeProjectIndexFixture(t *testing.T, preview bool) (scummVMFixture, string, string) {
	t.Helper()
	now := func() time.Time { return time.UnixMilli(1_786_000_000_000) }
	fixture := newScummVMFixtureAt(t, []string{"Two"}, now)
	if preview {
		created, err := fixture.service.CreateReviewPreview(t.Context(), ReviewPreviewRequest{
			ImportItemID: fixture.itemID, ActorUserID: scummVMActor, IdempotencyKey: "project-index-preview",
			ClientCapabilities: Capabilities{SecureContext: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.service.ReviewPreviewConfig(t.Context(), created.PreviewID, created.Capability); err != nil {
			t.Fatal(err)
		}
		return fixture, created.PreviewID, created.Capability
	}
	approved, err := fixture.importer.Approve(t.Context(), fixture.itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	created, err := fixture.service.Create(t.Context(), "scummvm-profile", CreateRequest{
		GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID, ClientCapabilities: Capabilities{SecureContext: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Config(t.Context(), created.LaunchID, created.Capability); err != nil {
		t.Fatal(err)
	}
	return fixture, created.LaunchID, created.Capability
}

func TestProjectIndexPreservesStorageCause(t *testing.T) {
	for _, preview := range []bool{false, true} {
		name := "product"
		if preview {
			name = "preview"
		}
		t.Run(name, func(t *testing.T) {
			fixture, id, capability := activeProjectIndexFixture(t, preview)
			if _, err := fixture.service.ProjectIndex(t.Context(), id, capability); err != nil {
				t.Fatal(err)
			}
			cause := errors.New("project source read unavailable")
			hits := 0
			table := "FROM launch_sessions launch"
			if preview {
				table = "FROM review_preview_sessions"
			}
			fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
				BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
					if hits == 0 && strings.Contains(query, table) && len(args) == 1 && args[0].Value == id {
						hits++
						return cause
					}
					return nil
				},
			})
			result, err := fixture.service.ProjectIndex(t.Context(), id, capability)
			if hits != 1 || !errors.Is(err, cause) || len(result.Contents) != 0 || result.SHA256 != "" {
				t.Fatalf("project source error lost: hits=%d error=%v bytes=%d", hits, err, len(result.Contents))
			}
		})
	}
}

func TestProjectIndexPreservesRequestCancellation(t *testing.T) {
	fixture, id, capability := activeProjectIndexFixture(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := fixture.service.ProjectIndex(ctx, id, capability)
	if !errors.Is(err, context.Canceled) || len(result.Contents) != 0 || result.SHA256 != "" {
		t.Fatalf("project cancellation lost: error=%v bytes=%d", err, len(result.Contents))
	}
}

func TestProjectIndexAuthorizesBeforeFilesAndPreservesFileFailure(t *testing.T) {
	for _, preview := range []bool{false, true} {
		name := "product"
		if preview {
			name = "preview"
		}
		t.Run(name, func(t *testing.T) {
			fixture, id, capability := activeProjectIndexFixture(t, preview)
			cause := errors.New("project files unavailable")
			hits := 0
			table := "FROM launch_content_files file"
			if preview {
				table = "FROM review_preview_files file"
			}
			fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
				BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
					if strings.Contains(query, table) && strings.Contains(query, "JOIN blobs") && len(args) > 0 && args[0].Value == id {
						hits++
						return cause
					}
					return nil
				},
			})
			denied, err := fixture.service.ProjectIndex(t.Context(), id, "wrong")
			if !errors.Is(err, ErrCredential) || hits != 0 || len(denied.Contents) != 0 {
				t.Fatalf("unauthorized files read: hits=%d error=%v", hits, err)
			}
			result, err := fixture.service.ProjectIndex(t.Context(), id, capability)
			if !errors.Is(err, cause) || hits != 1 || len(result.Contents) != 0 || result.SHA256 != "" {
				t.Fatalf("file cause lost: hits=%d error=%v", hits, err)
			}
		})
	}
}
