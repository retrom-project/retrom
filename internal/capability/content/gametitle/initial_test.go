package gametitle

import "testing"

func TestInitial(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "ASCII digit", title: "1945", want: "1"},
		{name: "ASCII uppercase", title: "Arcade", want: "A"},
		{name: "ASCII lowercase", title: "metal slug", want: "M"},
		{name: "simplified Han", title: "打击者1945", want: "D"},
		{name: "traditional Han", title: "遊戲", want: "Y"},
		{name: "symbol", title: "#Retro", want: "#"},
		{name: "emoji", title: "🎮 Game", want: "#"},
		{name: "non ASCII letter", title: "Éclair", want: "#"},
		{name: "empty", title: "", want: "#"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := Initial(test.title); got != test.want {
				t.Fatalf("Initial(%q) = %q, want %q", test.title, got, test.want)
			}
		})
	}
}
