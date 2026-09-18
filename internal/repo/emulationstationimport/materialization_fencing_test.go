package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"

	application "retrom/internal/model/emulationstationimport"
)

func TestMaterializationRepeatsExecutionItemAndSourceFence(t *testing.T) {
	t.Parallel()
	for _, field := range []string{
		"worker",
		"job version",
		"lease",
		"deadline",
		"item version",
		"root",
		"facts",
		"path",
		"size",
		"dimensions",
	} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			db, _, source, blob := materialDatabase(t)
			source = materialAsset(source)
			beforeRows := planRows(t, db)
			repo := NewMaterialization(db)
			before, err := repo.LoadMaterialSource(t.Context(), source.Key)
			if err != nil {
				t.Fatal(err)
			}
			switch field {
			case "worker":
				before.Before.Execution.WorkerID = "changed"
			case "job version":
				before.Before.Execution.JobVersion++
			case "lease":
				before.Before.Execution.LeaseUntilMS++
			case "deadline":
				before.Before.Execution.DeadlineAtMS++
			case "item version":
				before.Before.Item.Version++
			case "root":
				before.Before.Execution.RootDigest = "changed"
			case "facts":
				before.Source.Facts = "changed"
			case "path":
				before.Source.Path = "changed.png"
			case "size":
				before.Source.Size++
			case "dimensions":
				width := int64(2)
				before.Source.Width = &width
			}
			_, err = repo.CommitMaterialBinding(
				t.Context(),
				application.MaterialBinding{Before: before, Blob: blob, NowMS: 1100},
			)
			if !errors.Is(err, application.ErrVersionConflict) {
				t.Fatalf("accepted changed authority: %v", err)
			}
			if !reflect.DeepEqual(beforeRows, planRows(t, db)) {
				t.Fatal("mismatched material retained catalog or binding")
			}
		})
	}
}
