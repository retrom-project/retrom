package tagging_test

import (
	"testing"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/tagging"
	tagpersistence "retrom/internal/repo/tagging"
)

func TestBoundRelationsRollbackWithOuterTransaction(t *testing.T) {
	t.Parallel()
	database, service, _ := openTaggingTest(t)
	defer func() { cleanup.Error("close", database.Close()) }()
	tag, err := service.Create(t.Context(), testAdminID, "原子标签")
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := database.SQL.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback() }()
	scope := tagpersistence.Bind(transaction)
	if err := scope.Relations.Add(t.Context(), tagging.Assignment{
		Owner:      tagging.Owner{Kind: tagging.OwnerGame, ID: testGameID},
		References: []tagging.Reference{{TagID: tag.TagID, Name: tag.Name}}, ActorUserID: testAdminID, NowMS: 2000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := scope.Relations.TouchTags(t.Context(), testAdminID, []string{tag.TagID}, 2000); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	refs, err := service.References(t.Context(), []string{testGameID})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs[testGameID]) != 0 {
		t.Fatalf("outer rollback retained tags: %v", refs)
	}
	current, err := service.Get(t.Context(), tag.TagID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != tag.Version {
		t.Fatalf("outer rollback advanced tag version to %d", current.Version)
	}
}
