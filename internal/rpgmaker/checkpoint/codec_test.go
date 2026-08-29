package checkpoint

import (
	"encoding/binary"
	"errors"
	"testing"

	"retrom/internal/testassert"
)

func TestEncodeDecodeRoundTripIsCanonicalAndSorted(t *testing.T) {
	contents, err := Encode(EngineRPGMV, 21, []Entry{
		{Store: StoreRetromNative, Key: "z", MediaType: "application/octet-stream", Data: []byte("last")},
		{Store: StoreLocalStorage, Key: "存档", MediaType: "application/octet-stream", Data: []byte("first")},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := DecodeExpected(contents, Expected{Engine: EngineRPGMV, ResumeSlot: 21})
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return len(bundle.Entries) != 2 },
		func() bool { return bundle.Entries[0].Store != StoreLocalStorage },
		func() bool { return string(bundle.Entries[0].Data) != "first" },
		func() bool { return string(bundle.Entries[1].Data) != "last" },
	), "bundle=%#v err=%v", bundle, err)

	manifestLength := int(binary.BigEndian.Uint32(contents[len(Magic) : len(Magic)+4]))
	manifest := string(contents[len(Magic)+4 : len(Magic)+4+manifestLength])
	wantPrefix := `{"engine":"RPGMV","entries":[{"key":"存档","mediaType":"application/octet-stream","offset":0,"sha256":`
	if len(manifest) < len(wantPrefix) || manifest[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("manifest is not canonical: %s", manifest)
	}
}

func TestEasyRPGRequiresReservedSlot(t *testing.T) {
	if _, err := Encode(EngineRPG2000, 1, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want %v", err, ErrInvalid)
	}
	if _, err := Encode(EngineRPG2003, 100, nil); err != nil {
		t.Fatalf("reserved slot rejected: %v", err)
	}
}

func TestDecodeRejectsDigestMismatchTrailingAndNonCanonicalManifest(t *testing.T) {
	valid, err := Encode(EngineRPGMZ, 13, []Entry{{
		Store: StoreLocalForage, Key: "save", MediaType: "application/octet-stream", Data: []byte("payload"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte(nil), valid...)
	corrupt[len(corrupt)-1] ^= 1
	trailing := append(append([]byte(nil), valid...), 0)
	nonCanonical := makeNonCanonicalManifest(valid)
	for name, contents := range map[string][]byte{
		"digest": corrupt, "trailing": trailing, "non canonical": nonCanonical,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(contents); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want %v", err, ErrInvalid)
			}
		})
	}
}

func TestEncodeRejectsUnsafeKeysAndDuplicates(t *testing.T) {
	tests := map[string][]Entry{
		"filesystem traversal": {{Store: StoreFilesystem, Key: "../save", MediaType: "application/octet-stream"}},
		"non nfc":              {{Store: StoreLocalStorage, Key: "e\u0301", MediaType: "application/octet-stream"}},
		"duplicate": {
			{Store: StoreRetromNative, Key: "same", MediaType: "application/octet-stream"},
			{Store: StoreRetromNative, Key: "same", MediaType: "application/octet-stream"},
		},
	}
	for name, entries := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Encode(EngineRPGMV, 2, entries); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want %v", err, ErrInvalid)
			}
		})
	}
}

func TestDecodeExpectedRejectsEngineOrSlotMismatch(t *testing.T) {
	contents, err := Encode(EngineRPGMV, 9, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeExpected(contents, Expected{Engine: EngineRPGMZ, ResumeSlot: 9}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want %v", err, ErrInvalid)
	}
}

func TestDecodeAcceptsBrowserCanonicalVector(t *testing.T) {
	manifest := []byte(
		`{"engine":"RPGMV","entries":[{"key":"save","mediaType":"application/octet-stream",` +
			`"offset":0,"sha256":"ca358758f6d27e6cf45272937977a748fd88391db679ceda7dc7bf1f005ee879",` +
			`"sizeBytes":1,"store":"RETROM_NATIVE"}],"resumeSlot":21,"schemaVersion":1}`,
	)
	contents := make([]byte, 0, len(Magic)+4+len(manifest)+1)
	contents = append(contents, Magic...)
	var manifestLength [4]byte
	binary.BigEndian.PutUint32(manifestLength[:], uint32(len(manifest)))
	contents = append(contents, manifestLength[:]...)
	contents = append(contents, manifest...)
	contents = append(contents, 7)

	bundle, err := DecodeExpected(contents, Expected{Engine: EngineRPGMV, ResumeSlot: 21})
	if err != nil || len(bundle.Entries) != 1 || string(bundle.Entries[0].Data) != "\a" {
		t.Fatalf("bundle=%#v err=%v", bundle, err)
	}
}

func makeNonCanonicalManifest(valid []byte) []byte {
	manifestLength := int(binary.BigEndian.Uint32(valid[len(Magic) : len(Magic)+4]))
	manifestStart := len(Magic) + 4
	manifest := valid[manifestStart : manifestStart+manifestLength]
	changed := append([]byte(" "), manifest...)
	result := make([]byte, 0, len(valid)+1)
	result = append(result, Magic...)
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(changed)))
	result = append(result, size[:]...)
	result = append(result, changed...)
	result = append(result, valid[manifestStart+manifestLength:]...)
	return result
}
