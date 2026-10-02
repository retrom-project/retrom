package sourceimport

import (
	"strings"
	"testing"
)

func TestScanOutcomesPreserveDiagnosticsAndAllowCorrection(t *testing.T) {
	t.Parallel()
	source := scannerSourceFixture()
	source.contents["metadata.pegasus.txt"] = []byte("# header\nmissing colon\n")
	bad, err := NewScanner(source).Scan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	projection := bad.Projection()
	if projection.Summary.Outcome() != "INVALID" || len(projection.Summary.Diagnostics) != 1 {
		t.Fatalf("invalid scan: %#v", projection.Summary)
	}
	diagnostic := projection.Summary.Diagnostics[0]
	if diagnostic.RelativePath != "metadata.pegasus.txt" || diagnostic.Line == nil || *diagnostic.Line != 2 || diagnostic.Code != "PEGASUS_METADATA_SYNTAX_INVALID" || diagnostic.Message == "" {
		t.Fatalf("parser location/reason lost: %#v", diagnostic)
	}
	source.contents["metadata.pegasus.txt"] = []byte("collection: Test\ngame: Game\nfile: game.nes\n")
	good, err := NewScanner(source).Scan(t.Context())
	if err != nil || good.Projection().Summary.Outcome() != "READY" {
		t.Fatalf("corrected scan: %#v %v", good, err)
	}
	source.files = append(source.files, DiscoveredFile{Path: "bad/metadata.pegasus.txt", Name: "metadata.pegasus.txt", Size: 1, Facts: strings.Repeat("c", 64)})
	source.contents["bad/metadata.pegasus.txt"] = []byte("bad")
	partial, err := NewScanner(source).Scan(t.Context())
	if err != nil || partial.Projection().Summary.Outcome() != "PARTIAL" || len(partial.Items) != 1 {
		t.Fatalf("partial scan: %#v %v", partial, err)
	}
}

func TestEmptyAndMetadataAbsentScansRemainDistinct(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		files []DiscoveredFile
		want  string
	}{
		{name: "empty", want: "EMPTY"},
		{name: "no metadata", files: []DiscoveredFile{{Path: "game.nes", Name: "game.nes", Size: 1}}, want: "NO_METADATA"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := NewScanner(&scanSourceMemory{files: test.files}).Scan(t.Context())
			if err != nil || result.Projection().Summary.Outcome() != test.want {
				t.Fatalf("outcome=%s err=%v", result.Projection().Summary.Outcome(), err)
			}
		})
	}
}

func TestScanDiagnosticListIsBounded(t *testing.T) {
	t.Parallel()
	result := ScanResult{Metadata: make([]ScanMetadata, MaxScanDiagnostics+1)}
	for i := range result.Metadata {
		result.Metadata[i] = ScanMetadata{State: "INVALID", ErrorCode: "PEGASUS_METADATA_SYNTAX_INVALID"}
	}
	if got := len(result.Projection().Summary.Diagnostics); got != MaxScanDiagnostics {
		t.Fatalf("diagnostics=%d", got)
	}
}
