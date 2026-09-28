package platforminstance_test

import (
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/service/platforminstance"
)

func TestAppleIIRecommendationKeepsOneDirectoryWhenCoreChanges(t *testing.T) {
	t.Parallel()
	service, database := newService(t)
	if _, err := service.Apply(t.Context(), actor(), testUserID, "apple-recommendation-first"); err != nil {
		t.Fatal(err)
	}
	var count int
	var coreID, name string
	if err := dbapi.QueryRowContext(t.Context(), database, `
SELECT count(*), min(default_core_id), min(name) FROM platform_instances
WHERE platform_id='apple2' AND deleted_at_ms IS NULL`).Scan(&count, &coreID, &name); err != nil {
		t.Fatal(err)
	}
	if count != 1 || coreID != "apple2js" || name != "Apple II 游戏" {
		t.Fatalf("Apple II directories = %d, default core = %q, name = %q", count, coreID, name)
	}
	if _, err := database.ExecContext(t.Context(), `
UPDATE platform_instances SET default_core_id='mame_apple2',version=version+1
WHERE catalog_template_key='apple2/apple2js'`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(t.Context(), actor(), testUserID, "apple-recommendation-repeat"); err != nil {
		t.Fatal(err)
	}
	recommendations, err := service.Recommendations(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range recommendations.Items {
		if item.Platform.ID == "apple2" && item.State != platforminstance.StateCustomized {
			t.Fatalf("Apple II recommendation = %#v", item)
		}
	}
	if err := dbapi.QueryRowContext(t.Context(), database, `
SELECT count(*), min(default_core_id) FROM platform_instances
WHERE platform_id='apple2' AND deleted_at_ms IS NULL`).Scan(&count, &coreID); err != nil {
		t.Fatal(err)
	}
	if count != 1 || coreID != "mame_apple2" {
		t.Fatalf("reapplying recommendations changed the chosen core or created a directory: %d, %q", count, coreID)
	}
}
