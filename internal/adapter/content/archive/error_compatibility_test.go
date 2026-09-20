package archive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"retrom/internal/capability/format/importing"
	"retrom/internal/testkit/testsupport"
)

const oldArchiveErrorGoldenSHA = "8552e3066764146909bef9983bb3322090a4057af134b9ada7e6ddd3032938ae"

type oldErrorNode struct {
	Type     string
	Text     string
	Children []oldErrorNode
}

func archiveErrorShape(err error) oldErrorNode {
	node := oldErrorNode{Type: fmt.Sprintf("%T", err), Text: err.Error()}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			node.Children = append(node.Children, archiveErrorShape(child))
		}
	} else if wrapped, ok := err.(interface{ Unwrap() error }); ok && wrapped.Unwrap() != nil {
		node.Children = append(node.Children, archiveErrorShape(wrapped.Unwrap()))
	}
	return node
}

func TestArchiveErrorChainsMatchActualOldGo(t *testing.T) {
	t.Parallel()
	golden := loadOldErrorGolden(t)
	limits := importing.DefaultArchiveLimits()
	invalidPickle := make([]byte, 16)
	jsonHeader := rawASARHeader([]byte("{ "))
	pathHeader := rawASARHeader([]byte("{\"files\":{\"..\":{\"size\":1,\"offset\":\"0\"}}}"))
	cases := map[string]error{}
	_, _, cases["pickle lengths"] = readASARHeader(bytes.NewReader(invalidPickle), 100, limits)
	_, _, cases["ASAR JSON"] = readASARHeader(bytes.NewReader(jsonHeader), int64(len(jsonHeader)+100), limits)
	_, _, cases["ASAR path"] = readASARHeader(bytes.NewReader(pathHeader), int64(len(pathHeader)+100), limits)
	for _, test := range []struct {
		name    string
		headers []zip.FileHeader
		limits  importing.ArchiveLimits
	}{
		{"ZIP path", []zip.FileHeader{{Name: "../escape"}}, limits},
		{"ZIP duplicate", []zip.FileHeader{{Name: "a"}, {Name: "a"}}, limits},
		{"ZIP encrypted", []zip.FileHeader{{Name: "a", Flags: 1}}, limits},
		{"ZIP method", []zip.FileHeader{{Name: "a", Method: 99}}, limits},
		{"ZIP limit", []zip.FileHeader{{Name: "a", UncompressedSize64: 9}}, importing.ArchiveLimits{MaxEntries: 10, MaxEntryBytes: 1}},
	} {
		native := &zip.Reader{File: make([]*zip.File, len(test.headers))}
		for index, header := range test.headers {
			native.File[index] = &zip.File{FileHeader: header}
		}
		_, cases[test.name] = validateZIPDirectory(t.Context(), native, test.limits, true)
	}
	for _, test := range []struct {
		name  string
		limit int64
	}{{"member read", 10}, {"member limit", 1}} {
		monitor := &archiveReadMonitor{
			ctx: t.Context(), reader: &oneReadFault{contents: []byte("abc"), failure: io.ErrUnexpectedEOF}, limit: test.limit,
		}
		_, cases[test.name] = monitor.Read(make([]byte, 4))
	}
	factory := New(&testsupport.DiagnosticRecorder{})
	_, cases["flat directory"] = factory.ScanFlatZIP(t.Context(), writeZIP(t, "dir/", nil), limits)
	_, cases["scan nested"] = factory.ScanZIP(t.Context(), writeZIP(t, "data.bin", []byte{'P', 'K', 3, 4}), limits)
	if len(cases) != 12 || len(golden) != 12 {
		t.Fatal("old error case count changed")
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			if err == nil {
				t.Fatal("expected original failure")
			}
			actual := archiveErrorShape(err)
			if !reflect.DeepEqual(actual, golden[name]) {
				t.Fatalf("old error object graph changed: got %#v want %#v", actual, golden[name])
			}
		})
	}
}

func rawASARHeader(body []byte) []byte {
	payload := 4 + len(body)
	payload += (4 - payload%4) % 4
	result := make([]byte, 12+payload)
	binary.LittleEndian.PutUint32(result[:4], 4)
	binary.LittleEndian.PutUint32(result[4:8], uint32(payload+4))
	binary.LittleEndian.PutUint32(result[8:12], uint32(payload))
	binary.LittleEndian.PutUint32(result[12:16], uint32(len(body)))
	copy(result[16:], body)
	return result
}

func loadOldErrorGolden(t *testing.T) map[string]oldErrorNode {
	t.Helper()
	root := "testdata/old-errors"
	body, err := os.ReadFile(filepath.Join(root, "old-go-golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if archiveTestSHA(body) != oldArchiveErrorGoldenSHA {
		t.Fatal("old error golden changed")
	}
	var golden map[string]oldErrorNode
	if err := json.Unmarshal(body, &golden); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(root, "provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provenance struct {
		SourceSHA map[string]string `json:"source_sha256"`
		GoldenSHA string            `json:"golden_sha256"`
		Cases     int               `json:"cases"`
	}
	if err := json.Unmarshal(body, &provenance); err != nil {
		t.Fatal(err)
	}
	if provenance.GoldenSHA != oldArchiveErrorGoldenSHA || provenance.Cases != 12 {
		t.Fatal("old error provenance changed")
	}
	for name, want := range provenance.SourceSHA {
		path := filepath.Join(root, "old-source", name+".txt")
		if name == "capture_test.go" {
			path = filepath.Join(root, "capture.go.txt")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if archiveTestSHA(body) != want {
			t.Fatalf("old source changed: %s", name)
		}
	}
	return golden
}

func archiveTestSHA(body []byte) string {
	value := sha256.Sum256(body)
	return hex.EncodeToString(value[:])
}
