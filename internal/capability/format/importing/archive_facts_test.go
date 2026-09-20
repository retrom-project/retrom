package importing

import (
	"archive/zip"
	"errors"
	"io/fs"
	"reflect"
	"testing"
)

func TestArchiveFactsHaveNoResourceTypes(t *testing.T) {
	t.Parallel()
	for _, typ := range []reflect.Type{
		reflect.TypeFor[ArchiveMemberHeader](),
		reflect.TypeFor[ZIPDirectory](),
		reflect.TypeFor[ASARPickle](),
		reflect.TypeFor[ZIPHeaderFacts](),
		reflect.TypeFor[ZIPMember](),
		reflect.TypeFor[ElectronZIPLayout](),
		reflect.TypeFor[ASARMember](),
	} {
		assertArchiveFactType(t, typ, make(map[reflect.Type]bool))
	}
}

func assertArchiveFactType(t *testing.T, typ reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	if seen[typ] {
		return
	}
	seen[typ] = true
	if owner := typ.PkgPath(); owner != "" && owner != "retrom/internal/capability/format/importing" && owner != "io/fs" {
		t.Errorf("archive fact reaches external type %s", typ)
	}
	switch typ.Kind() {
	case reflect.Interface, reflect.Func, reflect.Chan, reflect.UnsafePointer, reflect.Uintptr, reflect.Invalid:
		t.Errorf("archive fact reaches executable or opaque type %s", typ)
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.String:
		return
	case reflect.Pointer, reflect.Slice, reflect.Array:
		assertArchiveFactType(t, typ.Elem(), seen)
	case reflect.Map:
		assertArchiveFactType(t, typ.Key(), seen)
		assertArchiveFactType(t, typ.Elem(), seen)
	case reflect.Struct:
		for index := range typ.NumField() {
			assertArchiveFactType(t, typ.Field(index).Type, seen)
		}
	}
}

func TestElectronLayoutUsesClosedHeaderFacts(t *testing.T) {
	t.Parallel()
	headers := []ZIPHeaderFacts{
		{Name: "Game/", Mode: fs.ModeDir},
		{Name: "Game/Game.exe", Method: zip.Store},
		{Name: "Game/resources/app.asar", Method: zip.Store},
		{
			Name: "Game/resources/app.asar.Unpacked/Native.DLL", Method: zip.Store,
			UncompressedSize64: 3, CompressedSize64: 3,
		},
	}
	directory, err := NewZIPDirectory(len(headers), DefaultArchiveLimits())
	if err != nil {
		t.Fatal(err)
	}
	items := make([]ZIPMember, 0, len(headers))
	for ordinal, header := range headers {
		item, isDirectory, err := directory.Add(ordinal, header)
		if err != nil {
			t.Fatal(err)
		}
		if !isDirectory {
			items = append(items, item)
		}
	}
	layout, detected, err := LocateElectronZIPLayout(items)
	if err != nil || !detected || layout.AppASAR.Ordinal != 2 || layout.Unpacked["native.dll"].Ordinal != 3 {
		t.Fatalf("layout = %#v, detected=%v, error=%v", layout, detected, err)
	}
	members := []ASARMember{{Path: "native.dll", Size: 3, Unpacked: true, Ordinal: 0}}
	before := append([]ASARMember(nil), members...)
	if err := ValidateUnpackedASARMembers(members, layout); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(members, before) {
		t.Fatal("binding validation changed pure ASAR member facts")
	}
	member := layout.Unpacked["native.dll"]
	member.Header.UncompressedSize64++
	layout.Unpacked["native.dll"] = member
	if err := ValidateUnpackedASARMembers(members, layout); !errors.Is(err, ErrElectronASARInvalid) {
		t.Fatalf("mismatched unpacked size = %v", err)
	}
}
