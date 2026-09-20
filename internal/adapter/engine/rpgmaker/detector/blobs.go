package detector

import (
	"context"
	"fmt"
	"io"
	"os"

	"retrom/internal/adapter/files/blobstore"
	policy "retrom/internal/capability/engine/rpgmaker/detector"
	model "retrom/internal/model/gamecontent"
)

type BlobDetector struct {
	blobs *blobstore.Store
}

var _ model.RPGMakerDetector = (*BlobDetector)(nil)

func NewBlobDetector(blobs *blobstore.Store) *BlobDetector {
	if blobs == nil {
		panic("RPG Maker BlobDetector requires a blob store")
	}
	return &BlobDetector{blobs: blobs}
}

func (detector *BlobDetector) DetectBlobs(
	_ context.Context, coreID string, files []model.RPGMakerBlobFile,
) (policy.Profile, error) {
	index := blobIndex{
		files: make([]policy.File, 0, len(files)), digests: make(map[string]string, len(files)), blobs: detector.blobs,
	}
	for _, file := range files {
		index.files = append(index.files, file.File)
		index.digests[file.File.Path] = file.SHA256
	}
	return detect(coreID, index)
}

type blobIndex struct {
	files   []policy.File
	digests map[string]string
	blobs   *blobstore.Store
}

func (index blobIndex) Files() []policy.File {
	return append([]policy.File(nil), index.files...)
}

func (index blobIndex) Open(logicalPath string) (io.ReadCloser, error) {
	digest, exists := index.digests[logicalPath]
	if !exists {
		return nil, os.ErrNotExist
	}
	reader, err := index.blobs.OpenDigest(digest)
	if err != nil {
		return nil, fmt.Errorf("open RPG replacement file: %w", err)
	}
	return reader, nil
}
