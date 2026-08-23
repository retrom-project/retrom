package emulationstationimport

import (
	"errors"
	"strings"
	"testing"

	"retrom/internal/testassert"
)

func TestSanitizeTechnicalDetailRemovesPrivateMaterialAndBoundsUTF8(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	service := &Service{roots: map[string]Root{"games": {path: "/private/source"}}}
	detail := service.sanitizeTechnicalDetail(errors.New(
		"read /private/source/private.rom\n" + digest + "\x00" + strings.Repeat("界", 600),
	))
	testassert.Falsef(t, strings.Contains(detail, "/private/source"), "root leaked: %q", detail)
	testassert.Falsef(t, strings.Contains(detail, digest), "digest leaked: %q", detail)
	testassert.Falsef(t, detail != "internal operation failed", "detail = %q", detail)
}
