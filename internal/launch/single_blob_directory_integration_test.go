//go:build integration

package launch

import (
	"testing"
	"time"

	dbapi "retrom/internal/database"

	platformpersistence "retrom/internal/persistence/platforminstance"
	"retrom/internal/service/platforminstance"
)

func createSingleBlobDirectory(t *testing.T, database dbapi.DB, input singleBlobCase, actorID string) string {
	t.Helper()
	service := platforminstance.New(platformpersistence.New(database), time.Now)
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
