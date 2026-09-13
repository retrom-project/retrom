package detector

import (
	"testing"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

func TestDetectRGSSGenerationsFromReversibleINI(t *testing.T) {
	tests := []struct {
		core       string
		scriptPath string
		generation Generation
	}{
		{core: "rpgmaker_xp", scriptPath: "Data/Scripts.rxdata", generation: RPGXP},
		{core: "rpgmaker_vx", scriptPath: "Data/Scripts.rvdata", generation: RPGVX},
		{core: "rpgmaker_vx_ace", scriptPath: "Data/Scripts.rvdata2", generation: RPGVXAce},
	}
	for _, test := range tests {
		t.Run(test.core, func(t *testing.T) {
			profile, err := Detect(test.core, rgssProject(test.scriptPath))
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			assertProfile(t, profile, test.generation, test.generation)
			if profile.EvidenceFamily != FamilyRGSS {
				t.Fatalf("EvidenceFamily = %q, want %q", profile.EvidenceFamily, FamilyRGSS)
			}
		})
	}

	cp932INI, _, err := transform.Bytes(japanese.ShiftJIS.NewEncoder(), []byte("[Game]\r\nScripts=Data\\Scripts.rxdata\r\nTitle=勇者\r\nRTP1=標準\r\n"))
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	project := memoryIndex{"Game.ini": cp932INI, "Data/Scripts.rxdata": []byte("scripts")}
	profile, err := Detect("rpgmaker_xp", project)
	if err != nil {
		t.Fatalf("Detect(CP932) error = %v", err)
	}
	if len(profile.RTPDependencies) != 1 || profile.RTPDependencies[0].Slot != 1 ||
		profile.RTPDependencies[0].DeclaredName != "標準" || profile.RTPDependencies[0].NormalizedName != "標準" {
		t.Fatalf("RTPDependencies = %#v", profile.RTPDependencies)
	}
	slotProject := rgssINI("[Game]\nScripts=Data/Scripts.rxdata\nRTP2=Standard\n", "Data/Scripts.rxdata")
	slotProfile, err := Detect("rpgmaker_xp", slotProject)
	if err != nil || len(slotProfile.RTPDependencies) != 1 || slotProfile.RTPDependencies[0].Slot != 2 {
		t.Fatalf("RTP2 slot profile = %#v, %v", slotProfile, err)
	}
}

func TestRGSSParserRejectsConflictsAndUnsafeInputs(t *testing.T) {
	tests := []struct {
		name    string
		project memoryIndex
		code    Code
	}{
		{name: "generation marker conflict", project: with(rgssProject("Data/Scripts.rxdata"), "Game.rvproj", []byte("marker")), code: CodeRGSSGenerationConflict},
		{name: "library conflict", project: rgssINI("[Game]\nScripts=Data/Scripts.rxdata\nLibrary=RGSS3 Player\n", "Data/Scripts.rxdata"), code: CodeRGSSGenerationConflict},
		{name: "root escape", project: rgssINI("[Game]\nScripts=../Scripts.rxdata\n", "Scripts.rxdata"), code: CodeINIInvalid},
		{name: "missing scripts", project: memoryIndex{"Game.ini": []byte("[Game]\nScripts=Data/Scripts.rxdata\n")}, code: CodeINIInvalid},
		{name: "duplicate conflicting key", project: rgssINI("[Game]\nScripts=Data/Scripts.rxdata\nScripts=Data/Other.rxdata\n", "Data/Scripts.rxdata"), code: CodeINIInvalid},
		{name: "irreversible CP932", project: memoryIndex{"Game.ini": {0x81}}, code: CodeINIEncodingUnsupported},
		{name: "two encrypted archives", project: with(with(rgssProject("Data/Scripts.rxdata"), "one.rgssad", []byte("a")), "two.rgssad", []byte("b")), code: CodeRGSSGenerationConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Detect("rpgmaker_xp", test.project)
			assertErrorCode(t, err, test.code)
		})
	}
}

func TestRGSSMatchingEncryptedArchiveCanReplaceLooseScripts(t *testing.T) {
	project := memoryIndex{
		"Game.ini":    []byte("[Game]\nScripts=Data/Scripts.rvdata2\n"),
		"Game.rgss3a": []byte("opaque archive"),
	}
	profile, err := Detect("rpgmaker_vx_ace", project)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	assertProfile(t, profile, RPGVXAce, RPGVXAce)
}

func rgssProject(scriptPath string) memoryIndex {
	return rgssINI("[Game]\nScripts="+scriptPath+"\nRTP1=\n", scriptPath)
}

func rgssINI(contents, scriptPath string) memoryIndex {
	return memoryIndex{"Game.ini": []byte(contents), scriptPath: []byte("opaque scripts")}
}

func with(project memoryIndex, name string, contents []byte) memoryIndex {
	cloned := cloneIndex(project)
	cloned[name] = contents
	return cloned
}
