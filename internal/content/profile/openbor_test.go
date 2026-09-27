package contentprofile

import "testing"

func TestOpenBORPackExtensions(t *testing.T) {
	for _, input := range []struct {
		name string
		want bool
	}{{"game.pak", true}, {"8MAN.PAK", true}, {"game.exe", false}, {"cover.png", false}, {"game.pak.txt", false}} {
		if got := AcceptsRaw("openbor", input.name); got != input.want {
			t.Errorf("AcceptsRaw(openbor,%q)=%v, want %v", input.name, got, input.want)
		}
	}
}
