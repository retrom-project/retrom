package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"retrom/internal/model"
)

func TestTagNameAndVersionConflictResponsesAreDistinct(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		err  error
		code string
	}{
		{model.ErrTagNameConflict, "TAG_NAME_CONFLICT"},
		{model.ErrConflict, "VERSION_CONFLICT"},
	} {
		response := httptest.NewRecorder()
		writeError(response, fmt.Errorf("tag operation: %w", item.err))
		var body struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusConflict || body.Code != item.code {
			t.Fatalf("status=%d body=%s want=%s", response.Code, response.Body, item.code)
		}
	}
}
