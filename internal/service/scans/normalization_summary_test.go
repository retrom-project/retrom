package scans

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"retrom/internal/model"
)

func TestNormalizationSummaryIsOptionalBoundedDiagnostics(t *testing.T) {
	t.Parallel()
	if err := validateNormalizationSummary(nil); err != nil {
		t.Fatal(err)
	}
	summary := normalizationSummary{
		Kind: "FORMAT_FAMILY", ConverterVersion: 1, SourceContentHash: strings.Repeat("a", 64),
		ArchiveCount: 2, VerifiedEntries: 20, MetadataCorrections: 1, OmittedPhysicalEntries: 1,
		FormatCounts: map[string]int64{"format": 2}, Warnings: []normalizationWarning{{Code: "METADATA_CORRECTED", Count: 1}},
	}
	if err := validateNormalizationSummary(&summary); err != nil {
		t.Fatal(err)
	}
	for name, modify := range map[string]func(*normalizationSummary){
		"bad hash":         func(s *normalizationSummary) { s.SourceContentHash = "not a digest" },
		"negative count":   func(s *normalizationSummary) { s.VerifiedEntries = -1 },
		"host path":        func(s *normalizationSummary) { s.Kind = "/private/path" },
		"warning overflow": func(s *normalizationSummary) { s.Warnings = make([]normalizationWarning, 9) },
		"format overflow": func(s *normalizationSummary) {
			s.FormatCounts = map[string]int64{"a": 1, "b": 1, "c": 1, "d": 1, "e": 1, "f": 1, "g": 1, "h": 1, "i": 1}
		},
	} {
		t.Run(name, func(t *testing.T) {
			invalid := summary
			modify(&invalid)
			if err := validateNormalizationSummary(&invalid); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("invalid diagnostic accepted: %v", err)
			}
		})
	}
}

func TestNormalizationAuditDetailsRemainStructuredAndBounded(t *testing.T) {
	t.Parallel()
	detail := map[string]any{
		"code": "METADATA_CORRECTED", "file": "data/archive.xp3", "member": "scenario/中文.ks",
		"sourceSha256": strings.Repeat("a", 64), "outputSha256": strings.Repeat("b", 64),
		"sourceSize": int64(0), "outputSize": model.MaximumMemberSize,
		"seed": uint32(0), "sourceChecksum": uint32(4294967295), "outputChecksum": uint32(1),
	}
	summary, err := decodeNormalizationAudit([]map[string]any{detail})
	if err != nil {
		t.Fatal(err)
	}
	if err = validateNormalizationSummary(summary); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Details []map[string]any `json:"details"`
	}
	if err = json.Unmarshal(body, &output); err != nil || len(output.Details) != 1 {
		t.Fatalf("structured audit detail lost: details=%+v err=%v", output.Details, err)
	}
	for key, value := range detail {
		want, _ := json.Marshal(value)
		got, _ := json.Marshal(output.Details[0][key])
		if string(got) != string(want) {
			t.Fatalf("audit field %s changed: got %s want %s", key, got, want)
		}
	}
}

func TestNormalizationAuditRejectsUnsafeOrInvalidDetails(t *testing.T) {
	t.Parallel()
	for name, field := range map[string]struct {
		key   string
		value any
	}{
		"empty code":          {"code", ""},
		"unsafe code":         {"code", "private/path"},
		"empty archive":       {"file", ""},
		"absolute archive":    {"file", "/private/data.xp3"},
		"parent archive":      {"file", "../data.xp3"},
		"unclean archive":     {"file", "archive/../data.xp3"},
		"drive archive":       {"file", "C:/private/data.xp3"},
		"long archive":        {"file", strings.Repeat("a", 4097)},
		"empty member":        {"member", ""},
		"absolute member":     {"member", "/private/scene.ks"},
		"parent member":       {"member", "../scene.ks"},
		"backslash member":    {"member", `scenario\scene.ks`},
		"control member":      {"member", "scenario/line\n.ks"},
		"invalid source SHA":  {"sourceSha256", strings.Repeat("A", 64)},
		"invalid output SHA":  {"outputSha256", "not a digest"},
		"negative source":     {"sourceSize", -1},
		"large output":        {"outputSize", model.MaximumMemberSize + 1},
		"fractional size":     {"sourceSize", 1.5},
		"negative seed":       {"seed", -1},
		"large checksum":      {"sourceChecksum", uint64(4294967296)},
		"fractional checksum": {"outputChecksum", 1.5},
	} {
		t.Run(name, func(t *testing.T) {
			detail := map[string]any{"code": "PHYSICAL_ENTRY_OMITTED", "file": "data.xp3", "member": "warning.txt"}
			detail[field.key] = field.value
			summary, err := decodeNormalizationAudit([]map[string]any{detail})
			if err == nil {
				err = validateNormalizationSummary(summary)
			}
			if err == nil {
				t.Fatal("invalid audit detail accepted")
			}
		})
	}
}

func TestNormalizationAuditCountAndEncodedSizeLimits(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		count      int
		pathLength int
		valid      bool
	}{
		{"maximum count", 2048, 1, true},
		{"count overflow", 2049, 1, false},
		{"maximum path", 1, 4096, true},
		{"below JSON budget", 1000, 4096, true},
		{"above JSON budget", 1100, 4096, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			details := make([]map[string]any, test.count)
			for i := range details {
				details[i] = map[string]any{
					"code": "OMITTED", "file": strings.Repeat("a", test.pathLength), "member": "warning.txt",
				}
			}
			summary, err := decodeNormalizationAudit(details)
			if err != nil {
				t.Fatal(err)
			}
			if err = validateNormalizationSummary(summary); (err == nil) != test.valid {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
		})
	}
}

func decodeNormalizationAudit(details any) (*normalizationSummary, error) {
	body, err := json.Marshal(map[string]any{
		"kind": "FORMAT_FAMILY", "converterVersion": 1,
		"sourceContentHash": strings.Repeat("a", 64), "details": details,
	})
	if err != nil {
		return nil, err
	}
	var summary normalizationSummary
	if err = json.Unmarshal(body, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

func TestNormalizationAuditSurvivesServiceLogHandlers(t *testing.T) {
	t.Parallel()
	summary, err := decodeNormalizationAudit([]map[string]any{{
		"code": "PHYSICAL_ENTRY_OMITTED", "file": "data.xp3", "member": "warning.txt",
		"sourceSha256": strings.Repeat("b", 64), "sourceSize": 157, "seed": 0, "sourceChecksum": 1754764004,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = validateNormalizationSummary(summary); err != nil {
		t.Fatal(err)
	}
	for name, handler := range map[string]func(*bytes.Buffer) slog.Handler{
		"text": func(buffer *bytes.Buffer) slog.Handler { return slog.NewTextHandler(buffer, nil) },
		"JSON": func(buffer *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(buffer, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			var buffer bytes.Buffer
			slog.New(handler(&buffer)).Info("content normalized", "operationId", "fixture", "normalization", summary)
			for _, value := range []string{"PHYSICAL_ENTRY_OMITTED", "warning.txt", strings.Repeat("b", 64), "1754764004"} {
				if !strings.Contains(buffer.String(), value) {
					t.Fatalf("audit value %q lost in service log: %s", value, buffer.String())
				}
			}
		})
	}
}
