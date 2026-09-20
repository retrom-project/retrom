package archive

import (
	"errors"
	"fmt"

	"retrom/internal/capability/format/importing"
)

func (cursor *asarCursor) completeUnpacked(
	member importing.ASARMember, entry importing.ArchiveEntry, consumeErr error,
) error {
	if consumeErr == nil {
		consumeErr = expectEOF(cursor.unpackedReader)
	}
	closeErr := cursor.unpackedReader.Close()
	cursor.unpackedReader = nil
	if consumeErr != nil || closeErr != nil {
		return errors.Join(consumeErr, closeErr, importing.ErrElectronASARInvalid)
	}
	item := cursor.archive.layout.Unpacked[importing.ASCIICaseFold(member.Path)]
	if entry.CRC32 != fmt.Sprintf("%08x", item.Header.CRC32) {
		return importing.ErrElectronASARInvalid
	}
	return nil
}
