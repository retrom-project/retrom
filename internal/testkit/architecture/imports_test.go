package architecture

import "testing"

func TestInPackageTreeMatchesOnlyTheRequestedPackageTree(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		path string
		root string
		want bool
	}{
		{name: "root", path: "retrom/internal/service", root: "retrom/internal/service", want: true},
		{name: "child", path: "retrom/internal/service/libraryimport", root: "retrom/internal/service", want: true},
		{name: "sibling prefix", path: "retrom/internal/services", root: "retrom/internal/service", want: false},
		{name: "nested root slash", path: "retrom/internal/service/libraryimport", root: "retrom/internal/service/", want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := inPackageTree(test.path, test.root); got != test.want {
				t.Fatalf("inPackageTree(%q, %q) = %t, want %t", test.path, test.root, got, test.want)
			}
		})
	}
}
