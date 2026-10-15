package scans

import "testing"

func TestBiosCandidateSelection(t *testing.T) {
	candidate := func(name, identity string, matched, total int) biosCandidate {
		return biosCandidate{Name: name, SHA256: name, Evidence: &biosCandidateEvidence{
			MemberIdentity: identity, MatchedMembers: matched, TotalMembers: total,
		}}
	}
	tests := []struct {
		name  string
		files []biosCandidate
		want  string
	}{
		{"same members", []biosCandidate{candidate("b.zip", "same", 2, 3), candidate("a.zip", "same", 2, 3)}, "a.zip"},
		{"DAT before entries", []biosCandidate{candidate("a.zip", "a", 1, 30), candidate("b.zip", "b", 2, 2)}, "b.zip"},
		{"entries break tie", []biosCandidate{candidate("a.zip", "a", 2, 2), candidate("b.zip", "b", 2, 3)}, "b.zip"},
		{"zero matches", []biosCandidate{candidate("a.zip", "a", 0, 1), candidate("b.zip", "b", 0, 3)}, "b.zip"},
		{"stable tie", []biosCandidate{candidate("b.zip", "b", 2, 3), candidate("a.zip", "a", 2, 3)}, "a.zip"},
		{"ambiguous raw files", []biosCandidate{{Name: "a.bin", SHA256: "a"}, {Name: "b.bin", SHA256: "b"}}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selected, ok := selectBiosCandidate(test.files)
			if selected.Name != test.want || ok != (test.want != "") {
				t.Fatalf("selected=%+v ok=%v want=%s", selected, ok, test.want)
			}
		})
	}
}
