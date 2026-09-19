package serverimport_test

import (
	"encoding/json"
	"testing"

	"retrom/internal/model/serverimport"
)

func TestDiscoveryCountsPreservesEncodedFields(t *testing.T) {
	for _, test := range []struct {
		name   string
		counts serverimport.DiscoveryCounts
		want   string
	}{
		{name: "zero", want: `{"Directories":0,"Files":0,"SkippedSpecial":0,"SkippedUnrepresentable":0}`},
		{
			name:   "all scan outcomes",
			counts: serverimport.DiscoveryCounts{Directories: 2, Files: 3, SkippedSpecial: 5, SkippedUnrepresentable: 7},
			want:   `{"Directories":2,"Files":3,"SkippedSpecial":5,"SkippedUnrepresentable":7}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.counts)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != test.want {
				t.Fatalf("discovery counts = %s, want %s", encoded, test.want)
			}
		})
	}
}
