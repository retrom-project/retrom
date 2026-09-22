package sourceimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"retrom/internal/cleanup"
	"retrom/internal/emulationstationmeta"
	"retrom/internal/pegasusmeta"
	"retrom/internal/serversource"
	application "retrom/internal/service/sourceimport"
)

func (source *Sources) VerifyMetadata(
	ctx context.Context,
	rootID, path string,
	evidence []application.MetadataEvidence,
) error {
	root, ok := source.roots[rootID]
	if !ok {
		return ErrSourceChanged
	}
	for _, expected := range evidence {
		if err := verifyMetadataFile(ctx, root, path, expected, source.acquireSourceReader); err != nil {
			return err
		}
	}
	return nil
}

func (service *Sources) acquireSourceReader(ctx context.Context) (func(), error) {
	if service.sourceReader != nil {
		return service.sourceReader(ctx)
	}
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire Source source reader: %w", err)
	}
	return release, nil
}

func verifyMetadataFile(
	ctx context.Context,
	root Root,
	path string,
	expected application.MetadataEvidence,
	acquire func(context.Context) (func(), error),
) error {
	release, err := acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire Source verification reader: %w", err)
	}
	defer release()
	file, before, err := serversource.OpenRelativeFile(root.path, path, expected.RelativePath)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSourceChanged, err)
	}
	defer func() { cleanup.Error("close Source verification file", file.Close()) }()
	if before.Size() != expected.SizeBytes || serversource.FactsDigest(before) != expected.FactsDigest {
		return ErrSourceChanged
	}
	if expected.ContentDigest == "" {
		if oversizedMetadata(expected) {
			return nil
		}
		return ErrSourceChanged
	}
	hash := sha256.New()
	reader := contextReader{ctx: ctx, reader: io.LimitReader(file, expected.SizeBytes+1)}
	count, err := io.Copy(hash, reader)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSourceChanged, err)
	}
	after, err := file.Stat()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSourceChanged, err)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if count != expected.SizeBytes || !serversource.SameFileFacts(before, after) || digest != expected.ContentDigest {
		return ErrSourceChanged
	}
	return nil
}

func oversizedMetadata(expected application.MetadataEvidence) bool {
	if expected.ParseState != "INVALID" {
		return false
	}
	switch expected.ErrorCode {
	case pegasusmeta.ErrTooLarge.Error():
		return expected.SizeBytes > pegasusmeta.MaxMetadataBytes
	case emulationstationmeta.ErrTooLarge.Error():
		return expected.SizeBytes > emulationstationmeta.MaxGameListBytes
	default:
		return false
	}
}
