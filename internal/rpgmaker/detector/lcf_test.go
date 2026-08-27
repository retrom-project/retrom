package detector

import (
	"bytes"
	"testing"
)

func TestLCFFixturesLockLibLCF081WriterLayout(t *testing.T) {
	wantDatabase := append([]byte{0x0b}, []byte("LcfDataBase")...)
	wantDatabase = append(wantDatabase, 0x16, 0x05, 0x0a, 0x02, 0x8f, 0x53, 0x00)
	if actual := makeLDBWithIDs(2003); !bytes.Equal(actual, wantDatabase) {
		t.Fatalf("RPG_RT.ldb fixture = %x; want %x", actual, wantDatabase)
	}
	wantMapTree := append([]byte{0x0a}, []byte("LcfMapTree")...)
	wantMapTree = append(wantMapTree, 1, 1, 0, 1, 1, 1, 1, 1, 1, 0)
	if actual := makeLMT(); !bytes.Equal(actual, wantMapTree) {
		t.Fatalf("RPG_RT.lmt fixture = %x; want %x", actual, wantMapTree)
	}
}

func TestLCFParserRejectsBoundAndStructureFailures(t *testing.T) {
	tests := []struct {
		name  string
		files memoryIndex
		code  Code
	}{
		{name: "truncated top chunk", files: replaceLDB(rpg2KProject(0), append(lcfHeader("LcfDataBase"), 0x16, 0x08, 0x00)), code: CodeLCFInvalid},
		{name: "overlong varint", files: replaceLDB(rpg2KProject(0), []byte{0x81, 0x80, 0x80, 0x80, 0x80, 0x00}), code: CodeLCFInvalid},
		{name: "duplicate ldb id", files: replaceLDB(rpg2KProject(0), makeLDBWithIDs(2003, 2003)), code: CodeLCFInvalid},
		{name: "unknown ldb id", files: replaceLDB(rpg2KProject(0), makeLDBWithIDs(2001)), code: CodeLCFGenerationUnknown},
		{name: "missing start map file", files: without(rpg2KProject(0), "Map0001.lmu"), code: CodeLMTInvalid},
		{name: "trailing lmt bytes", files: replaceLMT(rpg2KProject(0), append(makeLMT(), 0x01)), code: CodeLMTInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Detect("rpgmaker_2000", test.files)
			assertErrorCode(t, err, test.code)
		})
	}
}

func TestLCFMapTreeAcceptsLibLCFAdministrativeMapRecords(t *testing.T) {
	project := rpg2KProject(0)
	project["RPG_RT.lmt"] = makeLMTWithAdministrativeRecords()
	profile, err := Detect("rpgmaker_2000", project)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if profile.ExpectedGeneration != RPG2000 || profile.EvidenceConfidence != ConfidenceExact {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestRPGRTINIRequiresASCIIUnambiguousFullPackageFlag(t *testing.T) {
	tests := [][]byte{
		[]byte("[RPG_RT]\nFullPackageFlag=1\nFullPackageFlag=0\n"),
		[]byte("[RPG_RT]\nFullPackageFlag=１\n"),
		[]byte("[RPG_RT]\nFullPackageFlag=1\x00\n"),
		{0xff, 0xfe, '[', 'R', 'P', 'G', '_', 'R', 'T', ']'},
	}
	for index, contents := range tests {
		t.Run(string(rune('A'+index)), func(t *testing.T) {
			project := rpg2KProject(0)
			project["RPG_RT.ini"] = contents
			_, err := Detect("rpgmaker_2000", project)
			assertErrorCode(t, err, CodeINIInvalid)
		})
	}
}

func TestRPGRTINIFullPackageFlagProducesSelfContainedProfile(t *testing.T) {
	project := rpg2KProject(0)
	project["RPG_RT.ini"] = append([]byte{0xef, 0xbb, 0xbf}, []byte("[RPG_RT]\r\nFullPackageFlag=1\r\n")...)
	profile, err := Detect("rpgmaker_2000", project)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if !profile.SelfContained || len(profile.Requirements) != 1 ||
		profile.Requirements[0] != RequirementRuntimeValidation {
		t.Fatalf("self-contained profile = %#v", profile)
	}
}

func rpg2KProject(ldbID uint32) memoryIndex {
	return memoryIndex{
		"RPG_RT.ldb":  makeLDBWithIDs(ldbID),
		"RPG_RT.lmt":  makeLMT(),
		"Map0001.lmu": []byte("fixture map marker"),
	}
}

func makeLDBWithIDs(ids ...uint32) []byte {
	system := make([]byte, 0, 16)
	for _, id := range ids {
		if id == 0 {
			continue
		}
		value := encodeLCFInt(id)
		system = append(system, 0x0a)
		system = append(system, encodeLCFInt(uint32(len(value)))...)
		system = append(system, value...)
	}
	system = append(system, 0)
	contents := lcfHeader("LcfDataBase")
	contents = append(contents, 0x16)
	contents = append(contents, encodeLCFInt(uint32(len(system)))...)
	return append(contents, system...)
}

func makeLMT() []byte {
	const mapID = 1
	contents := lcfHeader("LcfMapTree")
	contents = append(contents, 1)
	contents = append(contents, encodeLCFInt(mapID)...)
	contents = append(contents, 0)
	contents = append(contents, 1)
	contents = append(contents, encodeLCFInt(mapID)...)
	contents = append(contents, encodeLCFInt(mapID)...)
	contents = append(contents, 1, 1)
	contents = append(contents, encodeLCFInt(mapID)...)
	return append(contents, 0)
}

func makeLMTWithAdministrativeRecords() []byte {
	const mapID = 1
	contents := lcfHeader("LcfMapTree")
	contents = append(contents, 3)
	for _, id := range []uint32{^uint32(0), 0, mapID} {
		contents = append(contents, encodeLCFInt(id)...)
		contents = append(contents, 0)
	}
	contents = append(contents, 1)
	contents = append(contents, encodeLCFInt(mapID)...)
	contents = append(contents, encodeLCFInt(mapID)...)
	contents = append(contents, 1, 1)
	contents = append(contents, encodeLCFInt(mapID)...)
	return append(contents, 0)
}

func lcfHeader(name string) []byte {
	return append(encodeLCFInt(uint32(len(name))), []byte(name)...)
}

func encodeLCFInt(value uint32) []byte {
	groups := []byte{byte(value & 0x7f)}
	for value >>= 7; value > 0; value >>= 7 {
		groups = append(groups, byte(value&0x7f)|0x80)
	}
	for left, right := 0, len(groups)-1; left < right; left, right = left+1, right-1 {
		groups[left], groups[right] = groups[right], groups[left]
	}
	return groups
}

func replaceLDB(project memoryIndex, contents []byte) memoryIndex {
	cloned := cloneIndex(project)
	cloned["RPG_RT.ldb"] = contents
	return cloned
}

func replaceLMT(project memoryIndex, contents []byte) memoryIndex {
	cloned := cloneIndex(project)
	cloned["RPG_RT.lmt"] = contents
	return cloned
}

func without(project memoryIndex, name string) memoryIndex {
	cloned := cloneIndex(project)
	delete(cloned, name)
	return cloned
}

func cloneIndex(project memoryIndex) memoryIndex {
	cloned := make(memoryIndex, len(project))
	for name, contents := range project {
		cloned[name] = append([]byte(nil), contents...)
	}
	return cloned
}
