package detector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	nxengine "retrom/internal/capability/engine/nxengine/detector"
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
	Case             string  `json:"case"`
	InputBytes       int     `json:"inputBytes,omitempty"`
	InputSHA256      string  `json:"inputSHA256,omitempty"`
	Snapshot         string  `json:"snapshot,omitempty"`
	Error            string  `json:"error,omitempty"`
	IsProjectInvalid bool    `json:"isProjectInvalid"`
	Panic            string  `json:"panic,omitempty"`
	IO               ioStats `json:"io"`
}

type oldGolden struct {
	SchemaVersion int          `json:"schemaVersion"`
	Results       []oldOutcome `json:"results"`
}

type compatibilityCase struct {
	files     []nxengine.File
	resources map[string]resourceSpec
	evidence  []byte
}

func TestOldGoGoldenCompatibility(t *testing.T) {
	t.Parallel()
	golden := loadGolden(t)
	cases := compatibilityCases()
	if len(golden.Results) != 19 || len(cases) != len(golden.Results) {
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
				if !errors.Is(err, nxengine.ErrProjectInvalid) {
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
		"Doukutsu.exe": {
			data: []byte("MZ"), readErr: errors.New("read failed"),
			failAfter: 1, closeErr: errors.New("close failed"),
		},
	}, stats)
	_, err := detector.Detect(context.Background(), projectFiles(nxBase(128)))
	if !errors.Is(err, nxengine.ErrProjectInvalid) {
		t.Fatalf("error=%v", err)
	}
	assertCloseEvent(t, reporter.events)
}

func TestDetectReadsResourcePathWithDefaultOpener(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	resourcePath := filepath.Join(root, "Doukutsu.exe")
	if err := os.WriteFile(resourcePath, []byte("MZ"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := projectFiles(nxBase(128))
	files[0].ResourcePath = resourcePath
	profile, err := New(&recordingReporter{}).Detect(context.Background(), files)
	if err != nil || profile.MarkerPath != "Doukutsu.exe" {
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

func projectFiles(files []nxengine.File) []libraryimport.ProjectProbeFile {
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
	profile nxengine.Profile,
	err error,
	stats *ioStats,
	evidence []byte,
) {
	t.Helper()
	actualError := ""
	if err != nil {
		actualError = err.Error()
	}
	if actualError != expected.Error ||
		errors.Is(err, nxengine.ErrProjectInvalid) != expected.IsProjectInvalid {
		t.Fatalf("error=%q invalid=%v; want %q invalid=%v",
			actualError, errors.Is(err, nxengine.ErrProjectInvalid),
			expected.Error, expected.IsProjectInvalid)
	}
	if expected.Error == nxengine.ErrProjectInvalid.Error() &&
		reflect.TypeOf(err) != reflect.TypeOf(nxengine.ErrProjectInvalid) {
		t.Fatalf("error concrete type=%T want %T", err, nxengine.ErrProjectInvalid)
	}
	actualSnapshot := ""
	if err == nil {
		encoded, marshalErr := nxengine.MarshalSnapshot(profile)
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
		events[0].Operation != "close nxengine project probe" ||
		events[0].Message != "*errors.errorString" {
		t.Fatalf("events=%#v", events)
	}
}

func nxBase(exeSize int64) []nxengine.File {
	return []nxengine.File{
		{Path: "Doukutsu.exe", Size: exeSize},
		{Path: "data/npc.tbl", Size: 1},
		{Path: "data/Stage/Start.pxm", Size: 1},
		{Path: "data/Stage/Start.tsc", Size: 1},
	}
}

func nxCount(count int) []nxengine.File {
	files := nxBase(128)
	for len(files) < count {
		files = append(files, nxengine.File{
			Path: fmt.Sprintf("data/extra/%04d.bin", len(files)), Size: 1,
		})
	}
	return files
}

func nxTotal(extra int64) []nxengine.File {
	return []nxengine.File{
		{Path: "Doukutsu.exe", Size: 16 * 1024 * 1024},
		{Path: "data/npc.tbl", Size: 1},
		{Path: "data/Stage/Start.pxm", Size: 1},
		{Path: "data/Stage/Start.tsc", Size: 1},
		{Path: "data/extra-a.bin", Size: 32 * 1024 * 1024},
		{Path: "data/extra-b.bin", Size: 16*1024*1024 - 3 + extra},
	}
}

func compatibilityCases() map[string]compatibilityCase {
	mz := []byte("MZ")
	return map[string]compatibilityCase{
		"exe_public": {
			files:     nxBase(128),
			resources: map[string]resourceSpec{"Doukutsu.exe": {data: mz, failAfter: -1}},
			evidence:  mz,
		},
		"file_count_exact_4096": {
			files:     nxCount(4096),
			resources: map[string]resourceSpec{"Doukutsu.exe": {data: mz, failAfter: -1}},
			evidence:  mz,
		},
		"file_count_4097_before_open": {
			files:     nxCount(4097),
			resources: map[string]resourceSpec{"Doukutsu.exe": {openErr: errors.New("must not open")}},
		},
		"declared_total_exact_64mib": {
			files:     nxTotal(0),
			resources: map[string]resourceSpec{"Doukutsu.exe": {data: mz, failAfter: -1}},
			evidence:  mz,
		},
		"declared_total_above_64mib": {
			files:     nxTotal(1),
			resources: map[string]resourceSpec{"Doukutsu.exe": {openErr: errors.New("must not open")}},
		},
		"zero_size_before_open": {
			files:     []nxengine.File{{Path: "Doukutsu.exe", Size: 0}},
			resources: map[string]resourceSpec{"Doukutsu.exe": {openErr: errors.New("must not open")}},
		},
		"individual_above_32mib_before_open": {
			files: []nxengine.File{
				{Path: "Doukutsu.exe", Size: 128},
				{Path: "data/npc.tbl", Size: 32*1024*1024 + 1},
			},
			resources: map[string]resourceSpec{"Doukutsu.exe": {openErr: errors.New("must not open")}},
		},
		"duplicate_before_open": {
			files:     append(nxBase(128), nxengine.File{Path: "doukutsu.exe", Size: 128}),
			resources: map[string]resourceSpec{"Doukutsu.exe": {openErr: errors.New("must not open")}},
		},
		"missing_marker": {
			files: nxBase(128)[1:],
		},
		"exe_size_127_before_open": {
			files:     nxBase(127),
			resources: map[string]resourceSpec{"Doukutsu.exe": {openErr: errors.New("must not open")}},
		},
		"exe_size_exact_16mib": {
			files:     nxBase(16 * 1024 * 1024),
			resources: map[string]resourceSpec{"Doukutsu.exe": {data: mz, failAfter: -1}},
			evidence:  mz,
		},
		"exe_size_above_16mib_before_open": {
			files:     nxBase(16*1024*1024 + 1),
			resources: map[string]resourceSpec{"Doukutsu.exe": {openErr: errors.New("must not open")}},
		},
		"open_failure_precedes_missing_assets": {
			files: []nxengine.File{{Path: "Doukutsu.exe", Size: 128}},
			resources: map[string]resourceSpec{
				"Doukutsu.exe": {openErr: errors.New("capture open failure")},
			},
		},
		"short_read_precedes_missing_assets": {
			files: []nxengine.File{{Path: "Doukutsu.exe", Size: 128}},
			resources: map[string]resourceSpec{
				"Doukutsu.exe": {data: []byte("M"), failAfter: -1},
			},
			evidence: []byte("M"),
		},
		"wrong_mz_precedes_missing_assets": {
			files: []nxengine.File{{Path: "Doukutsu.exe", Size: 128}},
			resources: map[string]resourceSpec{
				"Doukutsu.exe": {data: []byte("NZ"), failAfter: -1},
			},
			evidence: []byte("NZ"),
		},
		"missing_asset_after_valid_exe": {
			files: []nxengine.File{{Path: "Doukutsu.exe", Size: 128}},
			resources: map[string]resourceSpec{
				"Doukutsu.exe": {data: mz, failAfter: -1},
			},
			evidence: mz,
		},
		"physical_two_bytes_declared_128": {
			files:     nxBase(128),
			resources: map[string]resourceSpec{"Doukutsu.exe": {data: mz, failAfter: -1}},
			evidence:  mz,
		},
		"close_failure_ignored": {
			files: nxBase(128),
			resources: map[string]resourceSpec{
				"Doukutsu.exe": {
					data: mz, failAfter: -1, closeErr: errors.New("capture close failure"),
				},
			},
			evidence: mz,
		},
		"nil_reader_panics": {
			files:     nxBase(128),
			resources: map[string]resourceSpec{"Doukutsu.exe": {nilReader: true}},
		},
	}
}
