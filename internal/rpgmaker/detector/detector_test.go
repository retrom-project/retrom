package detector

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
)

type memoryIndex map[string][]byte

func (index memoryIndex) Files() []File {
	files := make([]File, 0, len(index))
	for name, contents := range index {
		files = append(files, File{Path: name, Size: int64(len(contents))})
	}
	return files
}

func (index memoryIndex) Open(name string) (io.ReadCloser, error) {
	contents, ok := index[name]
	if !ok {
		return nil, fmt.Errorf("missing memory file %q", name)
	}
	return io.NopCloser(bytes.NewReader(contents)), nil
}

func TestCoreGenerationIsFixedForEveryVisibleCore(t *testing.T) {
	wants := map[string]Generation{
		"rpgmaker_2000":   RPG2000,
		"rpgmaker_2003":   RPG2003,
		"rpgmaker_xp":     RPGXP,
		"rpgmaker_vx":     RPGVX,
		"rpgmaker_vx_ace": RPGVXAce,
		"rpgmaker_mv":     RPGMV,
		"rpgmaker_mz":     RPGMZ,
	}
	for coreID, want := range wants {
		got, err := GenerationForCore(coreID)
		if err != nil || got != want {
			t.Errorf("GenerationForCore(%q) = %q, %v; want %q", coreID, got, err, want)
		}
	}
	_, err := GenerationForCore("rpgmaker")
	assertErrorCode(t, err, CodeCoreUnsupported)
}

func TestDetectRPG2003ExactEvidence(t *testing.T) {
	profile, err := Detect("rpgmaker_2003", rpg2KProject(2003))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	assertProfile(t, profile, RPG2003, RPG2003)
	if profile.EvidenceFamily != FamilyRPG2K {
		t.Fatalf("EvidenceFamily = %q, want %q", profile.EvidenceFamily, FamilyRPG2K)
	}
}

func TestDetectRPG2003ExactEvidenceConflictsWithSelected2000(t *testing.T) {
	_, err := Detect("rpgmaker_2000", rpg2KProject(2003))
	assertErrorCode(t, err, CodeSelectedCoreMismatch)
	var detectionError *Error
	if !errors.As(err, &detectionError) || detectionError.EvidenceFamily != FamilyRPG2K ||
		detectionError.EvidenceGeneration == nil || *detectionError.EvidenceGeneration != RPG2003 {
		t.Fatalf("mismatch diagnostics = %#v", detectionError)
	}
}

func TestDetectRPG2KFamilyOnlyEvidenceKeepsSelected2000(t *testing.T) {
	profile, err := Detect("rpgmaker_2000", rpg2KProject(0))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if profile.Status != FamilyOnly || profile.EvidenceGeneration != nil || profile.EvidenceFamily != FamilyRPG2K {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestDetectRPG2KFamilyOnlyEvidenceKeepsSelected2003(t *testing.T) {
	profile, err := Detect("rpgmaker_2003", rpg2KProject(0))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if profile.Status != FamilyOnly || profile.ExpectedGeneration != RPG2003 {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestDetectRPG2KFamilyOnlyEvidenceConflictsWithRGSSCore(t *testing.T) {
	_, err := Detect("rpgmaker_xp", rpg2KProject(0))
	assertErrorCode(t, err, CodeSelectedCoreMismatch)
	var detectionError *Error
	if !errors.As(err, &detectionError) || detectionError.ExpectedGeneration != RPGXP ||
		detectionError.EvidenceFamily != FamilyRPG2K || detectionError.EvidenceGeneration != nil {
		t.Fatalf("mismatch diagnostics = %#v", detectionError)
	}
}

func TestDetectRejectsAmbiguousAndUnsupportedProjects(t *testing.T) {
	ambiguous := mvProject()
	for name, contents := range rgssProject("Data/Scripts.rxdata") {
		ambiguous[name] = contents
	}
	_, err := Detect("rpgmaker_mv", ambiguous)
	assertErrorCode(t, err, CodeGenerationAmbiguous)

	_, err = Detect("rpgmaker_mv", memoryIndex{"readme.txt": []byte("nothing")})
	assertErrorCode(t, err, CodeGenerationUnsupported)

	_, err = Detect("rpgmaker_mz", mvProject())
	assertErrorCode(t, err, CodeSelectedCoreMismatch)
	var detectionError *Error
	if !errors.As(err, &detectionError) || detectionError.EvidenceGeneration == nil ||
		*detectionError.EvidenceGeneration != RPGMV || detectionError.ExpectedGeneration != RPGMZ {
		t.Fatalf("exact mismatch diagnostics = %#v", detectionError)
	}
}

func TestFileIndexUsesNFKCCaseFoldAndRejectsCollisions(t *testing.T) {
	lowercase := memoryIndex{
		"rpg_rt.ldb":  makeLDBWithIDs(0),
		"rpg_rt.lmt":  makeLMT(),
		"map0001.lmu": []byte("map"),
	}
	profile, err := Detect("rpgmaker_2000", lowercase)
	if err != nil || profile.Status != FamilyOnly {
		t.Fatalf("case-folded detection = %#v, %v", profile, err)
	}
	if len(profile.MarkerPaths) == 0 || profile.MarkerPaths[0] != "map0001.lmu" {
		t.Fatalf("marker paths did not preserve original NFC casing: %#v", profile.MarkerPaths)
	}

	collision := memoryIndex{"Data/System.json": []byte("{}"), "Data/Ｓystem.json": []byte("{}")}
	_, err = Detect("rpgmaker_mv", collision)
	assertErrorCode(t, err, CodePathCollision)
}

func TestDetectionRejectsDeclaredFilesOverFormatLimit(t *testing.T) {
	project := rpg2KProject(0)
	oversize := sizedIndex{memoryIndex: project, sizes: map[string]int64{"RPG_RT.ldb": maxLDBBytes + 1}}
	_, err := Detect("rpgmaker_2000", oversize)
	assertErrorCode(t, err, CodeLCFInvalid)
}

type sizedIndex struct {
	memoryIndex
	sizes map[string]int64
}

func (index sizedIndex) Files() []File {
	files := index.memoryIndex.Files()
	for position := range files {
		if size, exists := index.sizes[files[position].Path]; exists {
			files[position].Size = size
		}
	}
	return files
}

func assertProfile(t *testing.T, profile Profile, expected, evidence Generation) {
	t.Helper()
	if profile.Status != Matched || profile.ExpectedGeneration != expected ||
		profile.EvidenceGeneration == nil || *profile.EvidenceGeneration != evidence {
		t.Fatalf("profile = %#v; want status=%q expected=%q evidence=%q", profile, Matched, expected, evidence)
	}
}

func assertErrorCode(t *testing.T, err error, want Code) {
	t.Helper()
	var detectionError *Error
	if !errors.As(err, &detectionError) {
		t.Fatalf("error = %v (%T); want *Error", err, err)
	}
	if detectionError.Code != want {
		t.Fatalf("error code = %q; want %q (error=%v)", detectionError.Code, want, err)
	}
}
