package importing_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"

	importing "retrom/internal/capability/format/importing"
)

type compatInputs struct {
	dir            string
	hashes         map[string]string
	normal         string
	empty          string
	duplicate      string
	folded         string
	traversal      string
	nested         string
	crc            string
	invalid        string
	nwjs           string
	badNWJS        string
	asar           string
	missing        string
	footer         string
	badRanges      string
	nestedASAR     string
	blockIntegrity string
	wrongIntegrity string
}

func zipBytes(items []zipInput) []byte {
	var body bytes.Buffer
	archive := zip.NewWriter(&body)
	for _, item := range items {
		header := &zip.FileHeader{Name: item.Name, Method: zip.Store}
		if item.Directory {
			header.SetMode(os.ModeDir | 0o755)
		}
		var writer io.Writer
		var err error
		if item.WrongCRC {
			header.UncompressedSize64 = uint64(len(item.Bytes))
			header.CompressedSize64 = header.UncompressedSize64
			header.CRC32 = crc32.ChecksumIEEE(item.Bytes) ^ 1
			writer, err = archive.CreateRaw(header)
		} else {
			writer, err = archive.CreateHeader(header)
		}
		must(err)
		_, err = writer.Write(item.Bytes)
		must(err)
	}
	must(archive.Close())
	return body.Bytes()
}

func asarBytes(header string, body []byte) []byte {
	data := []byte(header)
	payloadSize := 4 + len(data)
	payloadSize += (4 - payloadSize%4) % 4
	encoded := make([]byte, 12+payloadSize+len(body))
	binary.LittleEndian.PutUint32(encoded[0:4], 4)
	binary.LittleEndian.PutUint32(encoded[4:8], uint32(4+payloadSize))
	binary.LittleEndian.PutUint32(encoded[8:12], uint32(payloadSize))
	binary.LittleEndian.PutUint32(encoded[12:16], uint32(len(data)))
	copy(encoded[16:], data)
	copy(encoded[12+payloadSize:], body)
	return encoded
}

func electronBytes(header string, body []byte, unpacked bool, wrongCRC bool) []byte {
	items := []zipInput{{Name: "Game/Game.exe", Bytes: []byte("MZ synthetic text only")}, {Name: "Game/resources/app.asar", Bytes: asarBytes(header, body), WrongCRC: wrongCRC}}
	if unpacked {
		items = append(items, zipInput{Name: "Game/resources/app.asar.unpacked/u.node", Bytes: []byte("U")})
	}
	return zipBytes(items)
}

func peBytes(body []byte) []byte {
	prefix := make([]byte, 128+len(body))
	copy(prefix, "MZ")
	binary.LittleEndian.PutUint32(prefix[60:64], 64)
	copy(prefix[64:68], "PE\x00\x00")
	binary.LittleEndian.PutUint16(prefix[68:70], 0x8664)
	binary.LittleEndian.PutUint16(prefix[70:72], 1)
	copy(prefix[128:], body)
	return prefix
}

func compatFixtures(fixtures string) compatInputs {
	hashes := map[string]string{}
	write := func(name string, body []byte) string {
		file := filepath.Join(fixtures, name)
		must(os.WriteFile(file, body, 0o600))
		hashes[name] = sha(body)
		return file
	}
	normal := write("normal.zip", zipBytes([]zipInput{{Name: "dir/", Directory: true}, {Name: "dir/a.txt", Bytes: []byte("AAAA")}, {Name: "z.txt", Bytes: []byte("ZZ")}}))
	empty := write("empty.zip", zipBytes(nil))
	duplicate := write("duplicate.zip", zipBytes([]zipInput{{Name: "a", Bytes: []byte("A")}, {Name: "a", Bytes: []byte("B")}}))
	folded := write("casefold.zip", zipBytes([]zipInput{{Name: "A", Bytes: []byte("A")}, {Name: "a", Bytes: []byte("B")}}))
	traversal := write("traversal.zip", zipBytes([]zipInput{{Name: "a", Bytes: []byte("A")}, {Name: "../escape", Bytes: []byte("B")}}))
	nested := write("nested.zip", zipBytes([]zipInput{{Name: "inner.zip", Bytes: []byte{'P', 'K', 3, 4}}}))
	crc := write("crc.zip", zipBytes([]zipInput{{Name: "a", Bytes: []byte("CRC"), WrongCRC: true}}))
	invalid := write("invalid.bin", []byte("not an archive"))
	nwjs := write("nwjs.exe", peBytes(zipBytes([]zipInput{{Name: "index.html", Bytes: []byte("project")}})))
	badNWJS := write("bad-nwjs.exe", append([]byte("MZ invalid"), zipBytes([]zipInput{{Name: "index.html", Bytes: []byte("project")}})...))
	header := `{"files":{"a.txt":{"size":3,"offset":"2"},"z.txt":{"size":2,"offset":"0"},"u.node":{"size":1,"unpacked":true}}}`
	asar := write("asar.zip", electronBytes(header, []byte("ZZAAA"), true, false))
	missing := write("asar-missing.zip", electronBytes(header, []byte("ZZAAA"), false, false))
	footer := write("asar-footer.zip", electronBytes(header, []byte("ZZAAA"), true, true))
	badRanges := write("asar-overlap.zip", electronBytes(`{"files":{"a":{"size":2,"offset":"0"},"b":{"size":2,"offset":"1"}}}`, []byte("AAA"), false, false))
	nestedASAR := write("asar-nested.zip", electronBytes(`{"files":{"inner.zip":{"size":4,"offset":"0"}}}`, []byte{'P', 'K', 3, 4}, false, false))
	hash := sha([]byte("A"))
	wrongHash := strings.Repeat("0", 64)
	integrityHeader := fmt.Sprintf(`{"files":{"a":{"size":1,"offset":"0","integrity":{"algorithm":"SHA256","hash":%q,"blockSize":1,"blocks":[%q]}}}}`, hash, wrongHash)
	blockIntegrity := write("asar-block-integrity.zip", electronBytes(integrityHeader, []byte("A"), false, false))
	wrongIntegrity := write("asar-whole-integrity.zip", electronBytes(strings.Replace(integrityHeader, hash, wrongHash, 1), []byte("A"), false, false))

	return compatInputs{dir: fixtures, hashes: hashes, normal: normal, empty: empty, duplicate: duplicate, folded: folded, traversal: traversal, nested: nested, crc: crc, invalid: invalid, nwjs: nwjs, badNWJS: badNWJS, asar: asar, missing: missing, footer: footer, badRanges: badRanges, nestedASAR: nestedASAR, blockIntegrity: blockIntegrity, wrongIntegrity: wrongIntegrity}
}

func compatArchiveCases(inputs compatInputs) []archiveCase {
	limits := importing.DefaultArchiveLimits()
	return []archiveCase{
		{Name: "ZIP scan stable values", Kind: "zip-scan", Path: inputs.normal, Limits: limits},
		{Name: "ZIP consumer stable values", Kind: "zip-consume", Path: inputs.normal, Limits: limits},
		{Name: "ZIP duplicate rejects before consumer", Kind: "zip-consume", Path: inputs.duplicate, Limits: limits},
		{Name: "ZIP fold collision rejects before consumer", Kind: "zip-consume", Path: inputs.folded, Limits: limits},
		{Name: "ZIP traversal rejects before consumer", Kind: "zip-consume", Path: inputs.traversal, Limits: limits},
		{Name: "ZIP empty and canceled", Kind: "zip-scan", Path: inputs.empty, Limits: limits, Canceled: true},
		{Name: "ZIP invalid precedes cancellation", Kind: "zip-scan", Path: inputs.invalid, Limits: limits, Canceled: true},
		{Name: "ZIP valid canceled", Kind: "zip-consume", Path: inputs.normal, Limits: limits, Canceled: true},
		{Name: "ZIP nil consumer precedes opening", Kind: "zip-consume", Path: filepath.Join(inputs.dir, "missing"), Limits: limits, Mode: "nil"},
		{Name: "ZIP consumer failure before reading", Kind: "zip-consume", Path: inputs.normal, Limits: limits, Mode: "fail-before"},
		{Name: "ZIP consumer failure after reading", Kind: "zip-consume", Path: inputs.normal, Limits: limits, Mode: "fail-after"},
		{Name: "ZIP short consumer", Kind: "zip-consume", Path: inputs.normal, Limits: limits, Mode: "short"},
		{Name: "ZIP metadata CRC mismatch", Kind: "zip-consume", Path: inputs.normal, Limits: limits, Mode: "bad-crc"},
		{Name: "ZIP empty hashes current acceptance", Kind: "zip-consume", Path: inputs.normal, Limits: limits, Mode: "empty-hashes"},
		{Name: "ZIP CRC scan failure", Kind: "zip-scan", Path: inputs.crc, Limits: limits},
		{Name: "ZIP CRC consumer failure", Kind: "zip-consume", Path: inputs.crc, Limits: limits},
		{Name: "ZIP nested rejected", Kind: "zip-consume", Path: inputs.nested, Limits: limits},
		{Name: "ZIP opaque nested allowed", Kind: "zip-consume", Path: inputs.nested, Limits: importing.RPGMakerArchiveLimits()},
		{Name: "ZIP cancel after first", Kind: "zip-consume", Path: inputs.normal, Limits: limits, Mode: "cancel-after-first"},
		{Name: "Flat ZIP directory rejection", Kind: "flat", Path: inputs.normal, Limits: limits},
		{Name: "NWJS PE valid", Kind: "nwjs-validate", Path: inputs.nwjs, Limits: limits},
		{Name: "NWJS scan remains ZIP facts", Kind: "nwjs-scan", Path: inputs.nwjs, Limits: limits},
		{Name: "NWJS consume", Kind: "nwjs-consume", Path: inputs.nwjs, Limits: limits},
		{Name: "NWJS invalid before cancellation", Kind: "nwjs-consume", Path: inputs.badNWJS, Limits: limits, Canceled: true},
		{Name: "NWJS nil consumer first", Kind: "nwjs-consume", Path: inputs.badNWJS, Limits: limits, Mode: "nil"},
		{Name: "ASAR detect", Kind: "asar-detect", Path: inputs.asar, Limits: limits},
		{Name: "ASAR packed offset and final ordinal order", Kind: "asar-consume", Path: inputs.asar, Limits: limits},
		{Name: "ASAR bind before first consumer", Kind: "asar-consume", Path: inputs.missing, Limits: limits},
		{Name: "ASAR final packed CRC before unpacked", Kind: "asar-consume", Path: inputs.footer, Limits: limits},
		{Name: "ASAR overlapping ranges before consumer", Kind: "asar-consume", Path: inputs.badRanges, Limits: limits},
		{Name: "ASAR whole integrity mismatch", Kind: "asar-consume", Path: inputs.wrongIntegrity, Limits: limits},
		{Name: "ASAR block hashes only shape checked", Kind: "asar-consume", Path: inputs.blockIntegrity, Limits: limits},
		{Name: "ASAR nested fact remains opaque", Kind: "asar-consume", Path: inputs.nestedASAR, Limits: limits},
		{Name: "ASAR nil consumer first", Kind: "asar-consume", Path: inputs.invalid, Limits: limits, Mode: "nil"},
		{Name: "ASAR short consumer", Kind: "asar-consume", Path: inputs.asar, Limits: limits, Mode: "short"},
		{Name: "ASAR empty hashes rejected", Kind: "asar-consume", Path: inputs.asar, Limits: limits, Mode: "empty-hashes"},
		{Name: "ASAR unpacked callback error joins archive sentinel", Kind: "asar-consume", Path: inputs.asar, Limits: limits, Mode: "fail-unpacked"},
		{Name: "ASAR canceled header error precedence", Kind: "asar-consume", Path: inputs.asar, Limits: limits, Canceled: true},
		{Name: "ASAR callback failure", Kind: "asar-consume", Path: inputs.asar, Limits: limits, Mode: "fail-before"},
		{Name: "ASAR cancellation after packed member", Kind: "asar-consume", Path: inputs.asar, Limits: limits, Mode: "cancel-after-first"},
	}
}

func compatSevenZipCases(sourceRoot string, inputs compatInputs) []archiveCase {
	limits := importing.DefaultArchiveLimits()
	tests := make([]archiveCase, 0, 16)
	sevenRoot := filepath.Join(sourceRoot, "testdata/sevenzip")
	for _, name := range []string{"single", "ambiguous", "nested", "encrypted", "casefold", "symlink", "unsupported-coder"} {
		path := filepath.Join(sevenRoot, name+".7z")
		body, err := os.ReadFile(path)
		must(err)
		inputs.hashes["sevenzip/"+name+".7z"] = sha(body)
		tests = append(tests, archiveCase{Name: "7z " + name, Kind: "7z-scan", Path: path, Limits: limits})
	}
	singlePath := filepath.Join(sevenRoot, "single.7z")
	scanned, err := importing.ScanSevenZip(context.Background(), singlePath, limits)
	must(err)
	solidPath := filepath.Join(sevenRoot, "ambiguous.7z")
	solid, err := importing.ScanSevenZip(context.Background(), solidPath, limits)
	must(err)
	tests = append(tests,
		archiveCase{Name: "7z exact single extract", Kind: "7z-extract", Path: singlePath, Limits: limits, Entries: scanned},
		archiveCase{Name: "7z batch sorts ordinals", Kind: "7z-batch", Path: solidPath, Limits: limits, Entries: []importing.ArchiveEntry{solid[1], solid[0]}},
		archiveCase{Name: "7z batch consumer failure wins", Kind: "7z-batch", Path: solidPath, Limits: limits, Entries: solid, Mode: "fail-before"},
		archiveCase{Name: "7z empty batch avoids opening even canceled", Kind: "7z-batch", Path: filepath.Join(inputs.dir, "missing"), Limits: limits, Canceled: true},
		archiveCase{Name: "7z duplicate batch rejects before opening", Kind: "7z-batch", Path: filepath.Join(inputs.dir, "missing"), Limits: limits, Entries: []importing.ArchiveEntry{solid[0], solid[0]}},
		archiveCase{Name: "7z deadline maps resource limit", Kind: "7z-scan", Path: singlePath, Limits: limits, Mode: "expired"},
		archiveCase{Name: "7z valid canceled", Kind: "7z-scan", Path: singlePath, Limits: limits, Canceled: true},
		archiveCase{Name: "7z invalid precedes cancellation", Kind: "7z-scan", Path: inputs.invalid, Limits: limits, Canceled: true},
		archiveCase{Name: "7z nested allowed for RPG", Kind: "7z-scan", Path: filepath.Join(sevenRoot, "nested.7z"), Limits: importing.RPGMakerArchiveLimits()},
	)
	return tests
}
