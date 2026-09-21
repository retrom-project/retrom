package libraryimport

import (
	"reflect"
	"testing"

	"retrom/internal/testassert"
)

func TestNewInitialImportProgress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider string
		items    int
		rejected int
		want     initialImportProgress
	}{
		{
			name: "scrape accepted items", provider: "HASHEOUS", items: 2,
			want: initialImportProgress{state: "RUNNING", itemState: "SCRAPING", runningItems: 2},
		},
		{
			name: "all files rejected before scraping", provider: "HASHEOUS", rejected: 9,
			want: initialImportProgress{state: "PARTIAL_FAILURE", itemState: "REVIEW_PENDING"},
		},
		{
			name: "accepted items with rejected evidence", provider: "NONE", items: 2, rejected: 7,
			want: initialImportProgress{
				state: "PARTIAL_FAILURE", itemState: "REVIEW_PENDING", reviewPendingItems: 2,
			},
		},
		{
			name: "only ignored files", provider: "NONE",
			want: initialImportProgress{state: "COMPLETED", itemState: "REVIEW_PENDING", completed: true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := newInitialImportProgress(test.provider, test.items, test.rejected)
			testassert.Truef(t, reflect.DeepEqual(got, test.want), "newInitialImportProgress() = %#v, want %#v", got, test.want)
		})
	}
}
