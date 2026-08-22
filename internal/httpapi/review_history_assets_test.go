package httpapi

import "testing"

func TestSelectedReviewHistoryCoverURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		snapshot string
		expected string
	}{
		{name: "uploaded cover wins", snapshot: `{"selectedAssets":{"coverCandidateAssetId":"candidate","coverUploadedAssetId":"uploaded"}}`, expected: "/api/v1/admin/review-assets/uploaded"},
		{name: "candidate cover", snapshot: `{"selectedAssets":{"coverCandidateAssetId":"candidate","coverUploadedAssetId":null}}`, expected: "/api/v1/admin/review-assets/candidate"},
		{name: "source fallback required", snapshot: `{"selectedAssets":{"coverCandidateAssetId":null,"coverUploadedAssetId":null}}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual, err := selectedReviewHistoryCoverURL(test.snapshot)
			if err != nil {
				t.Fatalf("selectedReviewHistoryCoverURL() error = %v", err)
			}
			if test.expected == "" && actual.Valid {
				t.Fatalf("selectedReviewHistoryCoverURL() = %q, want invalid", actual.String)
			}
			if test.expected != "" && (!actual.Valid || actual.String != test.expected) {
				t.Fatalf("selectedReviewHistoryCoverURL() = %#v, want %q", actual, test.expected)
			}
		})
	}
}
