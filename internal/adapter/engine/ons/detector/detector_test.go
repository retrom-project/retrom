package detector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	ons "retrom/internal/capability/engine/ons/detector"
	"retrom/internal/model/diagnostics"
	libraryimport "retrom/internal/model/libraryimport"
)

type ioStats struct {
	OpenCalls      []string `json:"openCalls,omitempty"`
	ReadCalls      int      `json:"readCalls,omitempty"`
	ReadBytes      int      `json:"readBytes,omitempty"`
	MaxReadRequest int      `json:"maxReadRequest,omitempty"`
	CloseCalls     int      `json:"closeCalls,omitempty"`
}

type resourceSpec struct {
	data      []byte
	openErr   error
	readErr   error
	failAfter int
	chunk     int
	closeErr  error
	nilReader bool
}

type controlledReader struct {
	spec  resourceSpec
	stats *ioStats
	pos   int
}

func (reader *controlledReader) Read(buffer []byte) (int, error) {
	reader.stats.ReadCalls++
	if len(buffer) > reader.stats.MaxReadRequest {
		reader.stats.MaxReadRequest = len(buffer)
	}
	if reader.spec.failAfter >= 0 && reader.pos >= reader.spec.failAfter && reader.spec.readErr != nil {
		return 0, reader.spec.readErr
	}
	if reader.pos >= len(reader.spec.data) {
		return 0, io.EOF
	}
	limit := len(buffer)
	if reader.spec.chunk > 0 && limit > reader.spec.chunk {
		limit = reader.spec.chunk
	}
	if remaining := len(reader.spec.data) - reader.pos; limit > remaining {
		limit = remaining
	}
	if reader.spec.failAfter >= 0 && reader.spec.readErr != nil &&
		reader.pos+limit > reader.spec.failAfter {
		limit = reader.spec.failAfter - reader.pos
	}
	if limit == 0 {
		return 0, reader.spec.readErr
	}
	copy(buffer[:limit], reader.spec.data[reader.pos:reader.pos+limit])
	reader.pos += limit
	reader.stats.ReadBytes += limit
	return limit, nil
}

func (reader *controlledReader) Close() error {
	reader.stats.CloseCalls++
	return reader.spec.closeErr
}

type recordingReporter struct {
	events []diagnostics.DiagnosticEvent
}

func (reporter *recordingReporter) Report(_ context.Context, event diagnostics.DiagnosticEvent) {
	reporter.events = append(reporter.events, event)
}

type oldOutcome struct {
	Case             string          `json:"case"`
	InputBytes       int             `json:"inputBytes,omitempty"`
	InputSHA256      string          `json:"inputSHA256,omitempty"`
	Snapshot         string          `json:"snapshot,omitempty"`
	Error            string          `json:"error,omitempty"`
	IsProjectInvalid bool            `json:"isProjectInvalid"`
	Panic            string          `json:"panic,omitempty"`
	IO               ioStats         `json:"io"`
	Profile          json.RawMessage `json:"profile,omitempty"`
}

type oldGolden struct {
	SchemaVersion int          `json:"schemaVersion"`
	Results       []oldOutcome `json:"results"`
}

type compatibilityCase struct {
	files     []ons.File
	resources map[string]resourceSpec
	evidence  []byte
}

func TestOldGoGoldenCompatibility(t *testing.T) {
	t.Parallel()
	golden := loadGolden(t)
	cases := onsCompatibilityCases()
	if len(golden.Results) != 16 || len(cases) != len(golden.Results) {
		t.Fatalf("golden=%d cases=%d", len(golden.Results), len(cases))
	}
	for _, expected := range golden.Results {
		t.Run(expected.Case, func(t *testing.T) {
			spec, ok := cases[expected.Case]
			if !ok {
				t.Fatalf("missing case %q", expected.Case)
			}
			stats := &ioStats{}
			reporter := &recordingReporter{}
			detector := New(reporter)
			detector.openResource = controlledOpen(spec.resources, stats)
			profile, err := detector.Detect(context.Background(), projectFiles(spec.files))
			if expected.Panic != "" {
				if !errors.Is(err, ons.ErrProjectInvalid) {
					t.Fatalf("new nil-reader boundary error=%v", err)
				}
				if len(stats.OpenCalls) != 1 {
					t.Fatalf("nil-reader open calls=%v", stats.OpenCalls)
				}
				return
			}
			assertOutcome(t, expected, profile, err, stats, spec.evidence)
			if expected.Case == "close_failure_ignored" {
				assertCloseEvent(t, reporter.events)
			} else if len(reporter.events) != 0 {
				t.Fatalf("unexpected diagnostics=%#v", reporter.events)
			}
		})
	}
}

func TestDetectReportsCloseFailureWithoutReplacingReadFailure(t *testing.T) {
	t.Parallel()
	reporter := &recordingReporter{}
	stats := &ioStats{}
	detector := New(reporter)
	detector.openResource = controlledOpen(map[string]resourceSpec{
		"0.txt": {
			data: []byte("abcd"), readErr: errors.New("read failed"),
			failAfter: 2, closeErr: errors.New("close failed"),
		},
	}, stats)
	_, err := detector.Detect(context.Background(), projectFiles([]ons.File{
		{Path: "0.txt", Size: 4}, {Path: "font.ttf", Size: 1},
	}))
	if err == nil || err.Error() != "ONS_PROJECT_INVALID: script unavailable" {
		t.Fatalf("error=%v", err)
	}
	assertCloseEvent(t, reporter.events)
}

func TestDetectReadsResourcePathWithDefaultOpener(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	resourcePath := filepath.Join(root, "script.txt")
	if err := os.WriteFile(resourcePath, []byte("*define\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reporter := &recordingReporter{}
	profile, err := New(reporter).Detect(context.Background(), []libraryimport.ProjectProbeFile{
		{LogicalPath: "0.txt", DeclaredSize: 8, ResourcePath: resourcePath},
		{LogicalPath: "font.ttf", DeclaredSize: 1},
	})
	if err != nil || profile.ScriptEncoding != "utf8" {
		t.Fatalf("profile=%#v error=%v", profile, err)
	}
}

func loadGolden(t *testing.T) oldGolden {
	t.Helper()
	contents, err := os.ReadFile("testdata/old-go-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden oldGolden
	if err := json.Unmarshal(contents, &golden); err != nil {
		t.Fatal(err)
	}
	if golden.SchemaVersion != 1 {
		t.Fatalf("schemaVersion=%d", golden.SchemaVersion)
	}
	return golden
}

func controlledOpen(resources map[string]resourceSpec, stats *ioStats) func(string) (io.ReadCloser, error) {
	return func(name string) (io.ReadCloser, error) {
		stats.OpenCalls = append(stats.OpenCalls, name)
		spec, exists := resources[name]
		if !exists {
			return nil, errors.New("capture resource missing")
		}
		if spec.openErr != nil {
			return nil, spec.openErr
		}
		if spec.nilReader {
			return nil, nil
		}
		return &controlledReader{spec: spec, stats: stats}, nil
	}
}

func projectFiles(files []ons.File) []libraryimport.ProjectProbeFile {
	result := make([]libraryimport.ProjectProbeFile, len(files))
	for index, file := range files {
		result[index] = libraryimport.ProjectProbeFile{
			LogicalPath: file.Path, DeclaredSize: file.Size, ResourcePath: file.Path,
		}
	}
	return result
}

func assertOutcome(
	t *testing.T,
	expected oldOutcome,
	profile ons.Profile,
	err error,
	stats *ioStats,
	evidence []byte,
) {
	t.Helper()
	actualError := ""
	if err != nil {
		actualError = err.Error()
	}
	if actualError != expected.Error || errors.Is(err, ons.ErrProjectInvalid) != expected.IsProjectInvalid {
		t.Fatalf("error=%q invalid=%v; want %q invalid=%v",
			actualError, errors.Is(err, ons.ErrProjectInvalid), expected.Error, expected.IsProjectInvalid)
	}
	if expected.Error == ons.ErrProjectInvalid.Error() &&
		reflect.TypeOf(err) != reflect.TypeOf(ons.ErrProjectInvalid) {
		t.Fatalf("error concrete type=%T want %T", err, ons.ErrProjectInvalid)
	}
	actualSnapshot := ""
	if err == nil {
		encoded, marshalErr := ons.MarshalSnapshot(profile)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		actualSnapshot = string(encoded)
	}
	if actualSnapshot != expected.Snapshot {
		t.Fatalf("snapshot=%q want %q", actualSnapshot, expected.Snapshot)
	}
	if !reflect.DeepEqual(*stats, expected.IO) {
		t.Fatalf("io=%#v want %#v", *stats, expected.IO)
	}
	if evidence != nil {
		sum := sha256.Sum256(evidence)
		if len(evidence) != expected.InputBytes || hex.EncodeToString(sum[:]) != expected.InputSHA256 {
			t.Fatalf("evidence bytes=%d sha=%s", len(evidence), hex.EncodeToString(sum[:]))
		}
	}
}

func assertCloseEvent(t *testing.T, events []diagnostics.DiagnosticEvent) {
	t.Helper()
	if len(events) != 1 || events[0].Code != diagnostics.CleanupFailureCode ||
		events[0].Operation != "close ons project probe" || events[0].Message != "*errors.errorString" {
		t.Fatalf("events=%#v", events)
	}
}

func onsCompatibilityCases() map[string]compatibilityCase {
	utf8Text := []byte("*define\n;mode800\n")
	bomText := append([]byte{0xef, 0xbb, 0xbf}, []byte("*define\n")...)
	invalidUTF8 := []byte{0x81, 0xff}
	oneMiB := bytes.Repeat([]byte{'a'}, ons.MaxScriptProbeBytes)
	overOneMiB := append(append([]byte(nil), oneMiB...), 0xff)
	baseFiles := []ons.File{
		{Path: "0.txt", Size: int64(len(utf8Text))},
		{Path: "fonts/other.ttf", Size: 1},
		{Path: "default.ttf", Size: 1},
	}
	return map[string]compatibilityCase{
		"txt_utf8_public": {
			files: baseFiles, resources: map[string]resourceSpec{
				"0.txt": {data: utf8Text, failAfter: -1},
			}, evidence: utf8Text,
		},
		"txt_utf8_bom": {
			files:     []ons.File{{Path: "0.txt", Size: int64(len(bomText))}, {Path: "font.ttf", Size: 1}},
			resources: map[string]resourceSpec{"0.txt": {data: bomText, failAfter: -1}},
			evidence:  bomText,
		},
		"txt_invalid_utf8_defaults_gbk": {
			files:     []ons.File{{Path: "0.txt", Size: 2}, {Path: "font.ttf", Size: 1}},
			resources: map[string]resourceSpec{"0.txt": {data: invalidUTF8, failAfter: -1}},
			evidence:  invalidUTF8,
		},
		"dat_defaults_gbk_without_open": {
			files:     []ons.File{{Path: "nscript.dat", Size: 2}, {Path: "font.ttf", Size: 1}},
			resources: map[string]resourceSpec{"nscript.dat": {openErr: errors.New("must not open")}},
			evidence:  invalidUTF8,
		},
		"marker_and_font_priority": {
			files: []ons.File{
				{Path: "00.txt", Size: 1},
				{Path: "0.txt", Size: 1},
				{Path: "z.ttf", Size: 1},
				{Path: "default.ttf", Size: 1},
			},
			resources: map[string]resourceSpec{
				"0.txt":  {data: []byte("a"), failAfter: -1},
				"00.txt": {openErr: errors.New("must not open")},
			},
			evidence: []byte("a"),
		},
		"empty_path_before_open": {
			files:     []ons.File{{Path: "", Size: 0}, {Path: "0.txt", Size: 1}, {Path: "font.ttf", Size: 1}},
			resources: map[string]resourceSpec{"0.txt": {openErr: errors.New("must not open")}},
		},
		"negative_size_before_open": {
			files:     []ons.File{{Path: "0.txt", Size: -1}, {Path: "font.ttf", Size: 1}},
			resources: map[string]resourceSpec{"0.txt": {openErr: errors.New("must not open")}},
		},
		"duplicate_before_open": {
			files:     []ons.File{{Path: "0.txt", Size: 1}, {Path: "0.TXT", Size: 1}, {Path: "font.ttf", Size: 1}},
			resources: map[string]resourceSpec{"0.txt": {openErr: errors.New("must not open")}},
		},
		"missing_marker_before_open": {
			files: []ons.File{{Path: "font.ttf", Size: 1}},
		},
		"missing_font_before_open": {
			files:     []ons.File{{Path: "0.txt", Size: 1}},
			resources: map[string]resourceSpec{"0.txt": {openErr: errors.New("must not open")}},
		},
		"open_failure": {
			files:     []ons.File{{Path: "0.txt", Size: 1}, {Path: "font.ttf", Size: 1}},
			resources: map[string]resourceSpec{"0.txt": {openErr: errors.New("capture open failure")}},
		},
		"read_failure": {
			files: []ons.File{{Path: "0.txt", Size: 4}, {Path: "font.ttf", Size: 1}},
			resources: map[string]resourceSpec{
				"0.txt": {data: []byte("abcd"), readErr: errors.New("capture read failure"), failAfter: 2},
			},
			evidence: []byte("abcd"),
		},
		"close_failure_ignored": {
			files: []ons.File{{Path: "0.txt", Size: 1}, {Path: "font.ttf", Size: 1}},
			resources: map[string]resourceSpec{
				"0.txt": {data: []byte("a"), failAfter: -1, closeErr: errors.New("capture close failure")},
			},
			evidence: []byte("a"),
		},
		"exact_1mib": {
			files: []ons.File{{Path: "0.txt", Size: int64(len(oneMiB))}, {Path: "font.ttf", Size: 1}},
			resources: map[string]resourceSpec{
				"0.txt": {data: oneMiB, failAfter: -1, chunk: 65537},
			},
			evidence: oneMiB,
		},
		"beyond_1mib_unread_invalid_tail": {
			files: []ons.File{{Path: "0.txt", Size: 1}, {Path: "font.ttf", Size: 1}},
			resources: map[string]resourceSpec{
				"0.txt": {data: overOneMiB, failAfter: -1, chunk: 65537},
			},
			evidence: overOneMiB,
		},
		"nil_reader_panics": {
			files:     []ons.File{{Path: "0.txt", Size: 1}, {Path: "font.ttf", Size: 1}},
			resources: map[string]resourceSpec{"0.txt": {nilReader: true}},
		},
	}
}
