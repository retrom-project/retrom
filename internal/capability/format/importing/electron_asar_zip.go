package importing

import (
	"archive/zip"
	"fmt"
	"sort"
)

var ErrElectronASARInvalid = fmt.Errorf("%w: ELECTRON_ASAR_INVALID", ErrArchiveUnsafe)

func (member ASARMember) Header(method uint16) ArchiveMemberHeader {
	return ArchiveMemberHeader{Entry: ArchiveEntry{
		Ordinal: member.Ordinal, OriginalPath: member.Path, NormalizedPath: member.Path,
		ASCIICasefoldPath: ASCIICaseFold(member.Path), ArchiveFormat: "ELECTRON_ASAR",
		CompressionProfile: electronASARCompressionProfile(method),
	}, Unpacked: member.Unpacked}
}

func (member ASARMember) Complete(
	method uint16, content ArchiveContent, written int64, prefix []byte,
) (ArchiveEntry, error) {
	if written != member.Size || content.Size != member.Size || content.CRC32 == "" ||
		content.MD5 == "" || content.SHA1 == "" || content.SHA256 == "" {
		return ArchiveEntry{}, ErrElectronASARInvalid
	}
	if err := validateASARIntegrity(member, content); err != nil {
		return ArchiveEntry{}, err
	}
	entry := member.Header(method).Entry
	entry.Size = content.Size
	entry.CRC32, entry.MD5 = content.CRC32, content.MD5
	entry.SHA1, entry.SHA256 = content.SHA1, content.SHA256
	entry.NestedArchive = DetectNestedArchive(member.Path, prefix)
	return entry, nil
}

func SortASARMembersByOffset(members []ASARMember) {
	sort.Slice(members, func(left, right int) bool {
		if members[left].Unpacked != members[right].Unpacked {
			return !members[left].Unpacked
		}
		if members[left].Offset != members[right].Offset {
			return members[left].Offset < members[right].Offset
		}
		return members[left].Path < members[right].Path
	})
}

func electronASARCompressionProfile(method uint16) string {
	if method == zip.Store {
		return "ELECTRON_ASAR_STORE"
	}
	return "ELECTRON_ASAR_DEFLATE"
}

func invalidElectronASAR(reason string) error {
	return fmt.Errorf("%w: %s", ErrElectronASARInvalid, reason)
}
