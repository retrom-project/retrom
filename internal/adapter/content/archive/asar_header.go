package archive

import (
	"fmt"
	"io"

	"retrom/internal/capability/format/importing"
)

func readASARHeader(
	reader io.Reader, archiveSize int64, limits importing.ArchiveLimits,
) ([]importing.ASARMember, int64, error) {
	var prefix [16]byte
	if _, err := io.ReadFull(reader, prefix[:]); err != nil {
		return nil, 0, fmt.Errorf("%w: truncated pickle header", importing.ErrElectronASARInvalid)
	}
	layout, err := importing.ParseASARPickle(prefix, archiveSize)
	if err != nil {
		return nil, 0, err
	}
	header := make([]byte, layout.JSONSize)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, 0, fmt.Errorf("%w: truncated JSON header", importing.ErrElectronASARInvalid)
	}
	padding := make([]byte, layout.Padding)
	if _, err := io.ReadFull(reader, padding); err != nil || !importing.ValidASARPadding(padding) {
		return nil, 0, fmt.Errorf("%w: invalid pickle padding", importing.ErrElectronASARInvalid)
	}
	members, err := importing.DecodeASARHeader(header, archiveSize-layout.DataOffset, limits)
	if err != nil {
		return nil, 0, err
	}
	return members, layout.DataOffset, nil
}
