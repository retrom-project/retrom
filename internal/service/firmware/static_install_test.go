package firmware

import (
	"testing"
	"time"

	"retrom/internal/firmware"
)

func TestUploadWithoutHashEvidenceIsUnverifiedAndUsable(t *testing.T) {
	empty := ""
	for _, md5 := range []*string{nil, &empty} {
		memory := installFixture(t)
		memory.initial.MD5, memory.current.MD5 = md5, md5
		result, err := New(Dependencies{Repository: memory, Files: memory.files}, time.Now).
			Install(t.Context(), "requirement", 1, InstallRequest{UploadFileID: "file"})
		if err != nil || result.Status != "UNVERIFIED" || !result.Active {
			t.Fatalf("installation=%+v error=%v", result, err)
		}
		checks, ok := result.ValidationDetails["checks"].(firmware.StaticChecks)
		if !ok || checks.MD5 != "NOT_CHECKED" || checks.SHA256 != "NOT_CHECKED" {
			t.Fatalf("missing hash evidence presented as checked: %+v", result.ValidationDetails)
		}
	}
}
