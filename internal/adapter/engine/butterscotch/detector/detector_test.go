package detector

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	butterscotch "retrom/internal/capability/engine/butterscotch/detector"
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
	files     []butterscotch.File
	resources map[string]resourceSpec
	evidence  []byte
}

func TestOldGoGoldenCompatibility(t *testing.T) {
	t.Parallel()
	golden := loadGolden(t)
	cases := compatibilityCases()
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
				if !errors.Is(err, butterscotch.ErrProjectInvalid) {
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
		"data.win": {
			data: formHeader(8), readErr: errors.New("read failed"),
			failAfter: 4, closeErr: errors.New("close failed"),
		},
	}, stats)
	_, err := detector.Detect(context.Background(), projectFiles([]butterscotch.File{
		{Path: "data.win", Size: 16},
	}))
	if !errors.Is(err, butterscotch.ErrProjectInvalid) {
		t.Fatalf("error=%v", err)
	}
	assertCloseEvent(t, reporter.events)
}

func TestDetectReadsResourcePathWithDefaultOpener(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	resourcePath := filepath.Join(root, "data.win")
	if err := os.WriteFile(resourcePath, formHeader(8), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, err := New(&recordingReporter{}).Detect(
		context.Background(),
		[]libraryimport.ProjectProbeFile{
			{LogicalPath: "data.win", DeclaredSize: 16, ResourcePath: resourcePath},
		},
	)
	if err != nil || profile.MarkerPath != "data.win" {
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

func projectFiles(files []butterscotch.File) []libraryimport.ProjectProbeFile {
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
	profile butterscotch.Profile,
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
		errors.Is(err, butterscotch.ErrProjectInvalid) != expected.IsProjectInvalid {
		t.Fatalf("error=%q invalid=%v; want %q invalid=%v",
			actualError, errors.Is(err, butterscotch.ErrProjectInvalid),
			expected.Error, expected.IsProjectInvalid)
	}
	if expected.Error == butterscotch.ErrProjectInvalid.Error() &&
		reflect.TypeOf(err) != reflect.TypeOf(butterscotch.ErrProjectInvalid) {
		t.Fatalf("error concrete type=%T want %T", err, butterscotch.ErrProjectInvalid)
	}
	actualSnapshot := ""
	if err == nil {
		encoded, marshalErr := butterscotch.MarshalSnapshot(profile)
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
		events[0].Operation != "close butterscotch project probe" ||
		events[0].Message != "*errors.errorString" {
		t.Fatalf("events=%#v", events)
	}
}

func formHeader(declared uint32) []byte {
	header := make([]byte, 8)
	copy(header, "FORM")
	binary.LittleEndian.PutUint32(header[4:], declared)
	return header
}

func compatibilityCases() map[string]compatibilityCase {
	valid8, valid12 := formHeader(8), formHeader(12)
	wrong := []byte("NOPE\x08\x00\x00\x00")
	return map[string]compatibilityCase{
		"form_public": {
			files:     []butterscotch.File{{Path: "data.win", Size: 16}, {Path: "options.ini", Size: 1}},
			resources: map[string]resourceSpec{"data.win": {data: valid8, failAfter: -1}},
			evidence:  valid8,
		},
		"invalid_path_before_open": {
			files:     []butterscotch.File{{Path: "", Size: 1}, {Path: "data.win", Size: 16}},
			resources: map[string]resourceSpec{"data.win": {openErr: errors.New("must not open")}},
		},
		"negative_size_before_open": {
			files:     []butterscotch.File{{Path: "data.win", Size: -1}},
			resources: map[string]resourceSpec{"data.win": {openErr: errors.New("must not open")}},
		},
		"duplicate_before_open": {
			files:     []butterscotch.File{{Path: "data.win", Size: 16}, {Path: "DATA.WIN", Size: 16}},
			resources: map[string]resourceSpec{"data.win": {openErr: errors.New("must not open")}},
		},
		"missing_marker": {
			files: []butterscotch.File{{Path: "readme.txt", Size: 1}},
		},
		"undersized_marker_before_open": {
			files:     []butterscotch.File{{Path: "data.win", Size: 15}},
			resources: map[string]resourceSpec{"data.win": {openErr: errors.New("must not open")}},
		},
		"open_failure": {
			files:     []butterscotch.File{{Path: "data.win", Size: 16}},
			resources: map[string]resourceSpec{"data.win": {openErr: errors.New("capture open failure")}},
		},
		"short_read": {
			files:     []butterscotch.File{{Path: "data.win", Size: 16}},
			resources: map[string]resourceSpec{"data.win": {data: valid8[:7], failAfter: -1}},
			evidence:  valid8[:7],
		},
		"read_failure": {
			files: []butterscotch.File{{Path: "data.win", Size: 16}},
			resources: map[string]resourceSpec{
				"data.win": {data: valid8, readErr: errors.New("capture read failure"), failAfter: 4},
			},
			evidence: valid8,
		},
		"wrong_form": {
			files:     []butterscotch.File{{Path: "data.win", Size: 16}},
			resources: map[string]resourceSpec{"data.win": {data: wrong, failAfter: -1}},
			evidence:  wrong,
		},
		"declared_below_lower_bound": {
			files:     []butterscotch.File{{Path: "data.win", Size: 16}},
			resources: map[string]resourceSpec{"data.win": {data: formHeader(7), failAfter: -1}},
			evidence:  formHeader(7),
		},
		"declared_exact_upper_bound": {
			files:     []butterscotch.File{{Path: "data.win", Size: 20}},
			resources: map[string]resourceSpec{"data.win": {data: valid12, failAfter: -1}},
			evidence:  valid12,
		},
		"declared_above_upper_bound": {
			files:     []butterscotch.File{{Path: "data.win", Size: 20}},
			resources: map[string]resourceSpec{"data.win": {data: formHeader(13), failAfter: -1}},
			evidence:  formHeader(13),
		},
		"physical_eight_bytes_declared_twenty": {
			files:     []butterscotch.File{{Path: "data.win", Size: 20}},
			resources: map[string]resourceSpec{"data.win": {data: valid12, failAfter: -1}},
			evidence:  valid12,
		},
		"close_failure_ignored": {
			files: []butterscotch.File{{Path: "data.win", Size: 16}},
			resources: map[string]resourceSpec{
				"data.win": {data: valid8, failAfter: -1, closeErr: errors.New("capture close failure")},
			},
			evidence: valid8,
		},
		"nil_reader_panics": {
			files:     []butterscotch.File{{Path: "data.win", Size: 16}},
			resources: map[string]resourceSpec{"data.win": {nilReader: true}},
		},
	}
}
