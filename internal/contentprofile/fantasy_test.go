package contentprofile

import "testing"

func TestFantasyConsoleCartridgeExtensions(t *testing.T) {
	for _, input := range []struct {
		platform, name string
		want           bool
	}{
		{"tic80", "TICRIS.TIC", true},
		{"pico8", "Celeste.P8.PNG", true},
		{"pico8", "demo.p8", true},
		{"pico8", "cover.png", false},
		{"pico8", "demo.p8.png.zip", false},
		{"tic80", "demo.p8", false},
	} {
		if got := AcceptsRaw(input.platform, input.name); got != input.want {
			t.Errorf("AcceptsRaw(%q,%q)=%v, want %v", input.platform, input.name, got, input.want)
		}
	}
}
