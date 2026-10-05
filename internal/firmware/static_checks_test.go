package firmware

import "testing"

func TestStaticChecksOnlyCompareDeclaredRequirements(t *testing.T) {
	size := int64(3)
	facts := FileFacts{Basename: "bios.bin", SizeBytes: 3, MD5: "abcd", SHA1: "ef01", SHA256: "2345"}
	for _, test := range []struct {
		name              string
		expected          StaticExpectation
		status, md5, size string
	}{
		{"absent", StaticExpectation{}, "UNVERIFIED", "NOT_CHECKED", "NOT_CHECKED"},
		{"blank", StaticExpectation{MD5: "  "}, "UNVERIFIED", "NOT_CHECKED", "NOT_CHECKED"},
		{"size only", StaticExpectation{SizeBytes: &size}, "UNVERIFIED", "NOT_CHECKED", "MATCHED"},
		{"correct", StaticExpectation{SizeBytes: &size, MD5: "ABCD"}, "MATCHED", "MATCHED", "MATCHED"},
		{"wrong md5", StaticExpectation{MD5: "wrong"}, "HASH_WARNING", "MISMATCHED", "NOT_CHECKED"},
		{"wrong sha1 despite absent md5", StaticExpectation{SHA1: "wrong"}, "HASH_WARNING", "NOT_CHECKED", "NOT_CHECKED"},
		{"sha256 without md5", StaticExpectation{SHA256: "2345"}, "MATCHED", "NOT_CHECKED", "NOT_CHECKED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := EvaluateStatic(test.expected, facts)
			if got.Checks.MD5 != test.md5 || got.Checks.Size != test.size {
				t.Fatalf("checks=%+v", got.Checks)
			}
			if got.Status != test.status {
				t.Fatalf("status=%s want %s", got.Status, test.status)
			}
		})
	}
}
