package httpapi

import (
	"encoding/json"
	"testing"

	"retrom/internal/service/importdiscard"
)

func assertImportDiscardPage(t *testing.T, payload []byte, availableID string) {
	t.Helper()
	var page struct {
		Items []importListView `json:"items"`
	}
	if err := json.Unmarshal(payload, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 20 {
		t.Fatalf("discard projection page has %d items", len(page.Items))
	}
	for _, item := range page.Items {
		state := "UNAVAILABLE"
		if item.ID == availableID {
			state = "AVAILABLE"
		}
		if item.Discard.Kind != "IMPORT" || item.Discard.ImportID != item.ID || item.Discard.State != state {
			t.Fatalf("discard projection for %s: %+v", item.ID, item.Discard)
		}
	}
}

func assertImportDiscardDetail(t *testing.T, payload []byte, id, state string) {
	t.Helper()
	var detail struct {
		Discard importdiscard.Status `json:"discard"`
	}
	if err := json.Unmarshal(payload, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Discard.Kind != "IMPORT" || detail.Discard.ImportID != id || detail.Discard.State != state {
		t.Fatalf("detail discard projection: %+v", detail.Discard)
	}
}
