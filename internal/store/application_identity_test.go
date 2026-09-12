package store

import (
	"errors"
	"testing"

	"retrom/internal/recordstore"
)

func TestApplicationUserWritesPreserveIdentityAndLastAdministrator(t *testing.T) {
	t.Parallel()
	fixture := openEmulationStationSchemaFixture(t)
	db := fixture.database.SQL

	for _, tc := range []struct {
		name, set string
		values    []any
	}{
		{"last administrator", "role=?", []any{"USER"}},
		{"identity", "username=?", []any{"new-identity"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := lifecycleTransaction(t, db)
			_, err := recordstore.UpdateUsers(t.Context(), tx, recordstore.Update{
				Set:    tc.set,
				Values: tc.values,
				Scope: recordstore.Scope{
					Where: "id=?",
					Args:  []any{fixture.userID},
				},
			})
			if !errors.Is(err, recordstore.ErrInvariant) {
				t.Fatalf("invalid change error = %v", err)
			}
			if _, err := recordstore.UpdateUsers(t.Context(), tx, recordstore.Update{
				Set: "display_name='Still usable'",
				Scope: recordstore.Scope{
					Where: "id=?",
					Args:  []any{fixture.userID},
				},
			}); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var role, username string
			if err := db.QueryRowContext(t.Context(), "SELECT role,username FROM users WHERE id=?", fixture.userID).Scan(&role, &username); err != nil {
				t.Fatal(err)
			}
			if role != "ADMIN" || username != "schema-admin" {
				t.Fatalf("invalid write leaked: %s %s", role, username)
			}
		})
	}
	_, err := recordstore.DeleteUsers(t.Context(), db, recordstore.Scope{
		Where: "id=?",
		Args:  []any{fixture.userID},
	})
	if !errors.Is(err, recordstore.ErrInvariant) {
		t.Fatalf("physical deletion error = %v", err)
	}
}
