package detector

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type publicFixtureIndex struct {
	root  string
	files []File
}

func (index publicFixtureIndex) Files() []File { return append([]File(nil), index.files...) }

func (index publicFixtureIndex) Open(logicalPath string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(index.root, filepath.FromSlash(logicalPath)))
}

func TestPublicMaliciousShapeFixturesRemainDetectableWithoutExecutingNativePayloads(t *testing.T) {
	t.Parallel()
	_, filename, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(
		filepath.Dir(filename), "..", "..", "..", "testdata", "public-roms", "rpgmaker-smoke",
	))
	for _, test := range []struct {
		directory, core string
		generation      Generation
	}{
		{directory: "malicious-rpgmv", core: "rpgmaker_mv", generation: RPGMV},
		{directory: "malicious-rpgmz", core: "rpgmaker_mz", generation: RPGMZ},
	} {
		t.Run(test.directory, func(t *testing.T) {
			index := readPublicFixtureIndex(t, filepath.Join(root, test.directory))
			profile, err := Detect(test.core, index)
			if err != nil {
				t.Fatalf("Detect(%s) error = %v", test.directory, err)
			}
			if profile.EvidenceGeneration == nil || *profile.EvidenceGeneration != test.generation ||
				profile.ExpectedGeneration != test.generation || profile.Status != Matched {
				t.Fatalf("Detect(%s) profile = %#v", test.directory, profile)
			}
		})
	}
}

func TestPublicWrongCoreMatrixHasFortyTwoMismatches(t *testing.T) {
	t.Parallel()
	_, filename, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(
		filepath.Dir(filename), "..", "..", "..", "testdata", "public-roms", "rpgmaker-smoke",
	))
	var plan struct {
		WrongCore []struct {
			Fixture string `json:"fixture"`
			Targets []struct {
				CoreID             string `json:"coreId"`
				ExpectedCode       string `json:"expectedCode"`
				Accepted           bool   `json:"accepted"`
				EvidenceConfidence string `json:"evidenceConfidence"`
			} `json:"targets"`
		} `json:"wrongCore"`
	}
	contents, err := os.ReadFile(filepath.Join(root, "negative-matrix", "matrix.json"))
	if err != nil || json.Unmarshal(contents, &plan) != nil {
		t.Fatalf("read public security matrix: %v", err)
	}
	combinations := 0
	for _, source := range plan.WrongCore {
		index := readPublicFixtureIndex(t, filepath.Join(root, source.Fixture))
		for _, target := range source.Targets {
			combinations++
			profile, detectErr := Detect(target.CoreID, index)
			if target.Accepted {
				t.Fatalf("wrong-core target unexpectedly marked accepted: %s -> %s (%#v)", source.Fixture, target.CoreID, profile)
			}
			var detectionError *Error
			if !errors.As(detectErr, &detectionError) || detectionError.Code != CodeSelectedCoreMismatch ||
				target.ExpectedCode != string(CodeSelectedCoreMismatch) {
				t.Fatalf("mismatch %s -> %s error=%v", source.Fixture, target.CoreID, detectErr)
			}
		}
	}
	if combinations != 42 {
		t.Fatalf("wrong-core combinations=%d", combinations)
	}
}

func TestPublicNativeDependencyFixturesReachTheNativeDependencyGate(t *testing.T) {
	t.Parallel()
	_, filename, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(
		filepath.Dir(filename), "..", "..", "..", "testdata", "public-roms", "rpgmaker-smoke",
	))
	for _, directory := range []string{"external", "referenced-native"} {
		t.Run(directory, func(t *testing.T) {
			index := readPublicFixtureIndex(t, filepath.Join(root, "negative-matrix", directory))
			_, err := Detect("rpgmaker_mv", index)
			var detectionError *Error
			if !errors.As(err, &detectionError) || detectionError.Code != CodeNativeDependencyUnsupported {
				t.Fatalf("Detect(%s) error = %v, want %s", directory, err, CodeNativeDependencyUnsupported)
			}
		})
	}
}

func readPublicFixtureIndex(t *testing.T, root string) publicFixtureIndex {
	t.Helper()
	index := publicFixtureIndex{root: root}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		index.files = append(index.files, File{Path: filepath.ToSlash(relative), Size: info.Size()})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return index
}
