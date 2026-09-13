package libraryimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/engine/rpgmaker/detector"
)

func TestPreparedArtifactsUseGenerationSpecificValidationRole(t *testing.T) {
	cases := []struct {
		generation detector.Generation
		role, name string
	}{
		{detector.RPG2000, "RPG_EASYRPG_INDEX", "index.json"},
		{detector.RPG2003, "RPG_EASYRPG_INDEX", "index.json"},
		{detector.RPGXP, "RPG_MAKER_LAUNCH_BUNDLE", "game.mkxpz"},
		{detector.RPGVX, "RPG_MAKER_LAUNCH_BUNDLE", "game.mkxpz"},
		{detector.RPGVXAce, "RPG_MAKER_LAUNCH_BUNDLE", "game.mkxpz"},
	}
	for _, test := range cases {
		t.Run(string(test.generation), func(t *testing.T) {
			blobs := &preparedArtifactMemory{}
			input := []PreparedGroup{{
				RPGProfile:      &detector.Profile{ExpectedGeneration: test.generation},
				Sources:         []PreparedSource{{File: ImportFile{SHA256: "source", Size: 3}, LogicalName: "Data/game.bin"}},
				ValidationFiles: []PreparedValidationFile{{Role: "BIOS_BUNDLE", BlobID: "bios"}},
			}}
			output, err := NewImportArtifacts(blobs).Prepare(t.Context(), input, nil)
			if err != nil || len(output) != 1 || len(output[0].ValidationFiles) != 2 {
				t.Fatalf("output=%+v err=%v", output, err)
			}
			artifact := output[0].ValidationFiles[1]
			if artifact.Role != test.role || artifact.LogicalName != test.name || artifact.SortOrder != 1 || artifact.Artifact == nil || artifact.Artifact.Size == 0 || artifact.BlobID != "" {
				t.Fatalf("wrong prepared artifact: %+v", artifact)
			}
			if len(input[0].ValidationFiles) != 1 || output[0].ValidationFiles[0].BlobID != "bios" {
				t.Fatal("preparation changed existing validation")
			}
		})
	}
}

func TestPreparedArtifactsRequireSelectedArchiveMaterialization(t *testing.T) {
	ordinal := 7
	groups := []PreparedGroup{{
		RPGProfile: &detector.Profile{ExpectedGeneration: detector.RPGXP},
		Sources:    []PreparedSource{{File: ImportFile{SHA256: "archive"}, LogicalName: "game.bin", ArchiveBlobID: "zip", ArchiveOrdinal: &ordinal}},
	}}
	for _, archives := range [][]PreparedArchive{nil, {{BlobID: "zip"}}, {{BlobID: "other", Materialized: map[int]blobstore.Metadata{7: {SHA256: "entry", Size: 3}}}}} {
		result, err := NewImportArtifacts(&preparedArtifactMemory{}).Prepare(t.Context(), groups, archives)
		if !errors.Is(err, ErrInvalid) || result != nil {
			t.Fatalf("unmaterialized source result=%+v err=%v", result, err)
		}
	}
	blobs := &preparedArtifactMemory{}
	_, err := NewImportArtifacts(blobs).Prepare(t.Context(), groups, []PreparedArchive{{BlobID: "zip", Materialized: map[int]blobstore.Metadata{7: {SHA256: "entry", Size: 3}}}})
	if err != nil || blobs.opened != "entry" {
		t.Fatalf("opened=%s err=%v", blobs.opened, err)
	}
}

func TestPreparedArtifactsSkipNativeProjectsAndRejectUnknownOrCancelled(t *testing.T) {
	service := NewImportArtifacts(nil)
	for _, generation := range []detector.Generation{detector.RPGMV, detector.RPGMZ} {
		result, err := service.Prepare(t.Context(), []PreparedGroup{{RPGProfile: &detector.Profile{ExpectedGeneration: generation}}}, nil)
		if err != nil || len(result[0].ValidationFiles) != 0 {
			t.Fatalf("native artifact=%+v err=%v", result, err)
		}
	}
	groups := []PreparedGroup{{RPGProfile: &detector.Profile{ExpectedGeneration: "unknown"}}}
	if result, err := service.Prepare(t.Context(), groups, nil); !errors.Is(err, ErrInvalid) || result != nil {
		t.Fatalf("unknown result=%+v err=%v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := service.Prepare(ctx, groups, nil); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("cancel result=%+v err=%v", result, err)
	}
}

type preparedArtifactMemory struct{ opened string }

func (blobs *preparedArtifactMemory) OpenDigest(digest string) (io.ReadCloser, error) {
	blobs.opened = digest
	return io.NopCloser(bytes.NewReader([]byte("abc"))), nil
}

func (*preparedArtifactMemory) Put(reader io.Reader) (blobstore.Metadata, error) {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return blobstore.Metadata{}, err
	}
	digest := sha256.Sum256(contents)
	return blobstore.Metadata{SHA256: hex.EncodeToString(digest[:]), Size: int64(len(contents))}, nil
}
