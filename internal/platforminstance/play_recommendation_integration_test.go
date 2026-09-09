package platforminstance_test

import (
	"testing"

	"retrom/internal/platforminstance"
)

func TestPlayDirectoriesRequireManualCreation(t *testing.T) {
	t.Parallel()
	service, database := newService(t)
	recommendations, err := service.Recommendations(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range recommendations.Items {
		if item.DefaultCore.ID == "play" {
			t.Fatal("unstable Play! core must not have a recommended directory")
		}
	}
	if _, err := service.Apply(t.Context(), actor(), testUserID, "77777777-7777-4777-8777-777777777777"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_instances WHERE default_core_id='play'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("recommended directories created %d Play! directories", count)
	}
	manual, err := service.Create(t.Context(), actor(), platforminstance.CreateInput{
		PlatformID: "ps2", DefaultCoreID: "play", Name: "我的 PS2 游戏", SortOrder: 500,
	})
	if err != nil {
		t.Fatalf("manual Play! directory creation failed: %v", err)
	}
	if !manual.Enabled || manual.DefaultCoreID != "play" || manual.PlatformID != "ps2" {
		t.Fatalf("manual Play! directory = %#v", manual)
	}
	if _, err := service.Apply(t.Context(), actor(), testUserID, "88888888-8888-4888-8888-888888888888"); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `
SELECT count(*) FROM platform_instances
WHERE id=? AND name=? AND default_core_id='play' AND enabled=1
  AND deleted_at_ms IS NULL AND catalog_template_key IS NULL`, manual.ID, manual.Name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("applying recommendations changed the manual Play! directory")
	}
}
