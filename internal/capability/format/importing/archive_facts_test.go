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
		reflect.TypeFor[zipHeaderFacts](),
		reflect.TypeFor[validatedElectronZIPItem](),
		reflect.TypeFor[electronZIPLayout](),
		reflect.TypeFor[asarMember](),
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
	headers := []zipHeaderFacts{
		{Name: "Game/", Mode: fs.ModeDir},
		{Name: "Game/Game.exe", Method: zip.Store},
		{Name: "Game/resources/app.asar", Method: zip.Store},
		{
			Name: "Game/resources/app.asar.unpacked/Native.DLL", Method: zip.Store,
			UncompressedSize64: 3, CompressedSize64: 3,
		},
	}
	items, err := validateElectronZIPDirectory(headers, DefaultArchiveLimits())
	if err != nil {
		t.Fatal(err)
	}
	layout, detected, err := locateElectronZIPLayout(items)
	if err != nil || !detected || layout.appASAR.ordinal != 2 || layout.unpacked["native.dll"].ordinal != 3 {
		t.Fatalf("layout = %#v, detected=%v, error=%v", layout, detected, err)
	}
	members := []asarMember{{path: "native.dll", size: 3, unpacked: true, ordinal: 0}}
	before := append([]asarMember(nil), members...)
	if err := validateUnpackedASARMembers(members, layout); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(members, before) {
		t.Fatal("binding validation changed pure ASAR member facts")
	}
	member := layout.unpacked["native.dll"]
	member.header.UncompressedSize64++
	layout.unpacked["native.dll"] = member
	if err := validateUnpackedASARMembers(members, layout); !errors.Is(err, ErrElectronASARInvalid) {
		t.Fatalf("mismatched unpacked size = %v", err)
	}
}
