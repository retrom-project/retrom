package model

import (
	"strings"
	"testing"
)

func TestPackageFactsRejectUnsafeNamesAndIntervals(t *testing.T) {
	valid := GameFile{
		Role: "project_package", SizeBytes: 1024, SHA256: strings.Repeat("a", 64),
		Members: []GameFileMember{{
			LogicalPath: "目录/# file.txt", DataOffset: 100, SizeBytes: 5,
			SHA256: strings.Repeat("b", 64), MediaType: "text/plain",
		}},
	}
	if err := ValidatePackage(valid); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".", "../escape", "/absolute", "bad\\file", "line\nbreak", string([]byte{255}), strings.Repeat("x", 1025)} {
		file := valid
		file.Members = append([]GameFileMember(nil), valid.Members...)
		file.Members[0].LogicalPath = name
		if err := ValidatePackage(file); err == nil {
			t.Fatalf("allowed unsafe path %q", name)
		}
	}
	for _, offset := range []int64{-1, 1024, 1 << 62} {
		file := valid
		file.Members = append([]GameFileMember(nil), valid.Members...)
		file.Members[0].DataOffset = offset
		if err := ValidatePackage(file); err == nil {
			t.Fatalf("allowed bad interval %d", offset)
		}
	}
	valid.SizeBytes = MaximumPackageSize + 1
	if err := ValidatePackage(valid); err == nil {
		t.Fatal("allowed oversized physical object")
	}
}
