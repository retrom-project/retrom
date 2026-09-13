//go:build integration

package launch

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"retrom/internal/testkit/testsupport"
)

func assertTyranoContentReadCauses(t *testing.T, service *Service, id string, preview bool) {
	t.Helper()
	original := service.database
	defer func() { service.database = original }()
	cause := errors.New("TyranoScript content storage unavailable")
	hits := 0
	service.database = testsupport.OpenSQLFaultDatabase(t, original, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			statement := strings.Contains(query, "FROM launch_sessions l")
			if preview {
				statement = strings.Contains(query, "WITH preview_files AS (")
			}
			if statement && len(args) >= 3 && args[0].Value == id {
				hits++
				return cause
			}
			return nil
		},
	})
	content, err := service.TyranoScriptProjectContentAuthorized(t.Context(), id, "index.html", preview)
	if hits != 1 || !errors.Is(err, cause) || content.Digest != "" {
		t.Errorf("Tyrano content preview=%t hits=%d error=%v", preview, hits, err)
	}
	service.database = original
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	content, err = service.TyranoScriptProjectContentAuthorized(ctx, id, "index.html", preview)
	if !errors.Is(err, context.Canceled) || content.Digest != "" {
		t.Errorf("Tyrano content cancellation preview=%t error=%v", preview, err)
	}
}
