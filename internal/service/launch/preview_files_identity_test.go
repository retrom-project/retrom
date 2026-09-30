package launch

import "testing"

func TestPreviewRestoreFilesCompareEveryFrozenDimension(t *testing.T) {
	t.Parallel()
	path := "/bios.bin"
	file := PreviewFile{
		Role: "EXTERNAL_FILE", LogicalName: "bios.bin", FileRecord: "blob",
		VirtualPath: &path, SortOrder: 1,
	}
	cases := []struct {
		name   string
		change func(*PreviewFile)
	}{
		{"role", func(f *PreviewFile) { f.Role = "PARENT" }},
		{"name", func(f *PreviewFile) { f.LogicalName = "different.bin" }},
		{"blob", func(f *PreviewFile) { f.FileRecord = "changed" }},
		{"path", func(f *PreviewFile) { p := "/new.bin"; f.VirtualPath = &p }},
		{"order", func(f *PreviewFile) { f.SortOrder = 2 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := file
			test.change(&changed)
			if samePreviewFiles([]PreviewFile{file}, []PreviewFile{changed}) {
				t.Fatal("changed frozen file accepted")
			}
		})
	}
	if !samePreviewFiles([]PreviewFile{file, {FileRecord: "other"}}, []PreviewFile{{FileRecord: "other"}, file}) {
		t.Fatal("read order changed file identity")
	}
	if samePreviewFiles([]PreviewFile{file, file}, []PreviewFile{file, {FileRecord: "other"}}) {
		t.Fatal("duplicate erased a missing file")
	}
}
