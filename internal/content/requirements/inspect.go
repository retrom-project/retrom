package requirements

import (
	"context"
	"errors"
	"fmt"
	"io"

	"retrom/internal/importing"

	"retrom/internal/content/diagnostic"
	"retrom/internal/format/nintendo3ds"
)

// Inspect reads only the bounded format facts required by the selected policy.
// Parsing and admission stay separate; callers retain facts for later rechecks.
func (policy *Policy) Inspect(ctx context.Context, reader io.ReaderAt, size int64, name string,
) (*Facts, *diagnostic.Rejection, error) {
	if policy == nil {
		return nil, nil, nil
	}
	switch policy.Kind {
	case FlycastCartridge:
		entries, err := importing.ScanZIPReader(ctx, reader, size, importing.DefaultArchiveLimits())
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, fmt.Errorf("inspect content: %w", ctx.Err())
			}
			return nil, &diagnostic.Rejection{Code: "FLYCAST_ARCHIVE_INVALID", RelativePath: name}, nil
		}
		facts := &Facts{Archive: make([]ArchiveMember, 0, len(entries))}
		for _, entry := range entries {
			facts.Archive = append(facts.Archive, ArchiveMember{
				Name: entry.NormalizedPath, SizeBytes: entry.Size, CRC32: entry.CRC32,
			})
		}
		return facts, policy.Evaluate(*facts, name), nil
	case Decrypted3DS:
		parsed, err := nintendo3ds.Read(reader, size)
		if errors.Is(err, nintendo3ds.ErrInvalid) {
			return nil, &diagnostic.Rejection{Code: "THREEDS_CONTAINER_INVALID", RelativePath: name}, nil
		}
		if err != nil {
			return nil, nil, fmt.Errorf("inspect content: %w", err)
		}
		facts := &Facts{Nintendo3DS: &parsed}
		return facts, policy.Evaluate(*facts, name), nil
	default:
		return nil, nil, ErrPolicyInvalid
	}
}

var ErrPolicyInvalid = errors.New("CONTENT_REQUIREMENT_INVALID")
