//go:build integration

package launch

import (
	"encoding/json"
	"testing"
	"time"

	dbapi "retrom/internal/database"

	platformpersistence "retrom/internal/persistence/platforminstance"
	"retrom/internal/service/platforminstance"
)

func createSingleBlobDirectory(t *testing.T, database dbapi.DB, input singleBlobCase, actorID string) string {
	t.Helper()
	service := platforminstance.New(platformpersistence.New(database), time.Now)
	receipt, err := service.CreateIdempotent(t.Context(), platforminstance.AuditActor{
		Kind: "USER", UserID: actorID, RequestID: "single-blob-directory",
	}, actorID, "single-blob-"+input.core, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", platforminstance.CreateInput{
		PlatformID: input.platform, DefaultCoreID: input.core, Name: "手动游戏目录",
	}, true)
	if err != nil {
		t.Fatalf("create manual %s/%s directory: %v", input.platform, input.core, err)
	}
	var directory platforminstance.Instance
	if err := json.Unmarshal(receipt.Body, &directory); err != nil {
		t.Fatal(err)
	}
	return directory.ID
}
