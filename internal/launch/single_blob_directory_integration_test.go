//go:build integration

package launch

import (
	"database/sql"
	"testing"
	"time"

	"retrom/internal/platforminstance"
)

func createSingleBlobDirectory(t *testing.T, database *sql.DB, input singleBlobCase, actorID string) string {
	t.Helper()
	service := platforminstance.New(database, time.Now)
	directory, err := service.Create(t.Context(), platforminstance.AuditActor{
		Kind: "USER", UserID: actorID, RequestID: "single-blob-directory",
	}, platforminstance.CreateInput{
		PlatformID: input.platform, DefaultCoreID: input.core, Name: "手动游戏目录", SortOrder: 500,
	})
	if err != nil {
		t.Fatalf("create manual %s/%s directory: %v", input.platform, input.core, err)
	}
	return directory.ID
}
