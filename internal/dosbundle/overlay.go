package dosbundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"sort"
	"strings"

	"retrom/internal/zipentry"
)

var ErrInvalid = errors.New("DOS_BUNDLE_INVALID")

const (
	localHeaderSignature   = 0x04034b50
	centralHeaderSignature = 0x02014b50
	endSignature           = 0x06054b50
	endMinimumSize         = 22
	maximumCommentSize     = 1<<16 - 1
)

type Overlay struct {
	parts  []part
	size   int64
	offset int64
}

type part struct {
	bytes  []byte
	source io.ReaderAt
	start  int64
	size   int64
}

type launcherFile struct {
	name     string
	contents []byte
}

type archiveEntry struct {
	start       int
	next        int
	rawName     string
	decodedName string
	localOffset uint32
}

func New(source io.ReaderAt, size int64, selectedEntry string) (*Overlay, error) {
	if selectedEntry == "" {
		return nil, ErrInvalid
	}
	return newOverlay(source, size, selectedEntry)
}

func NewMenu(source io.ReaderAt, size int64) (*Overlay, error) {
	return newOverlay(source, size, "")
}

//nolint:gocyclo // Each branch validates an independent classic-ZIP structural invariant.
func newOverlay(source io.ReaderAt, size int64, selectedEntry string) (*Overlay, error) {
	if size < endMinimumSize {
		return nil, ErrInvalid
	}
	endOffset, end, err := readEnd(source, size)
	if err != nil {
		return nil, err
	}
	entries := binary.LittleEndian.Uint16(end[10:12])
	centralSize := int64(binary.LittleEndian.Uint32(end[12:16]))
	centralOffset := int64(binary.LittleEndian.Uint32(end[16:20]))
	if binary.LittleEndian.Uint16(end[4:6]) != 0 || binary.LittleEndian.Uint16(end[6:8]) != 0 ||
		binary.LittleEndian.Uint16(end[8:10]) != entries || entries == math.MaxUint16 ||
		centralOffset == math.MaxUint32 || centralSize == math.MaxUint32 ||
		centralOffset < 0 || centralSize < 0 || centralOffset+centralSize != endOffset {
		return nil, ErrInvalid
	}
	central := make([]byte, centralSize)
	if _, err := source.ReadAt(central, centralOffset); err != nil {
		return nil, ErrInvalid
	}
	parsedEntries, err := parseCentralDirectory(central, entries)
	if err != nil {
		return nil, err
	}
	mappedNames, mappedSelectedEntry, err := planLegacyNameRewrites(parsedEntries, selectedEntry)
	if err != nil {
		return nil, err
	}
	resolutionCentral, err := rewriteCentralNames(central, parsedEntries, mappedNames)
	if err != nil {
		return nil, err
	}
	launchers, err := overlayLaunchers(resolutionCentral, entries, mappedSelectedEntry)
	if err != nil {
		return nil, err
	}
	local, centralEntries, err := encodeLauncherRecords(launchers)
	if err != nil {
		return nil, err
	}
	offsetDelta, ok := checkedUint32(len(local))
	if !ok {
		return nil, ErrInvalid
	}
	localParts, localSize, rewrittenOffsets, err := rewriteLocalNames(
		source, centralOffset, parsedEntries, mappedNames,
	)
	if err != nil {
		return nil, err
	}
	central, retainedEntries, err := patchCentralDirectory(
		central, parsedEntries, mappedNames, rewrittenOffsets, offsetDelta,
	)
	if err != nil {
		return nil, err
	}
	return assembleOverlay(
		size, endOffset, end, local, centralEntries, localParts, localSize,
		central, retainedEntries, len(launchers),
	)
}

func assembleOverlay(
	sourceSize int64,
	endOffset int64,
	end []byte,
	launcherLocal []byte,
	launcherCentral []byte,
	localParts []part,
	localSize int64,
	central []byte,
	retainedEntries uint16,
	launcherCountValue int,
) (*Overlay, error) {
	commentLength := int(binary.LittleEndian.Uint16(end[20:22]))
	if int64(endMinimumSize+commentLength) != sourceSize-endOffset {
		return nil, ErrInvalid
	}
	outputEnd := append([]byte(nil), end...)
	launcherCount, ok := checkedUint16(launcherCountValue)
	if !ok || retainedEntries > math.MaxUint16-launcherCount {
		return nil, ErrInvalid
	}
	outputEntries := retainedEntries + launcherCount
	binary.LittleEndian.PutUint16(outputEnd[8:10], outputEntries)
	binary.LittleEndian.PutUint16(outputEnd[10:12], outputEntries)
	newCentralSize := len(launcherCentral) + len(central)
	newCentralOffset := int64(len(launcherLocal)) + localSize
	centralOffsetValue, offsetOK := checkedInt64Uint32(newCentralOffset)
	if newCentralSize > math.MaxUint32 || !offsetOK {
		return nil, ErrInvalid
	}
	binary.LittleEndian.PutUint32(outputEnd[12:16], uint32(newCentralSize))
	binary.LittleEndian.PutUint32(outputEnd[16:20], centralOffsetValue)
	parts := make([]part, 0, 1+len(localParts)+3)
	parts = append(parts, part{bytes: launcherLocal, size: int64(len(launcherLocal))})
	parts = append(parts, localParts...)
	parts = append(parts,
		part{bytes: launcherCentral, size: int64(len(launcherCentral))},
		part{bytes: central, size: int64(len(central))},
		part{bytes: outputEnd, size: int64(len(outputEnd))},
	)
	var outputSize int64
	for _, outputPart := range parts {
		outputSize += outputPart.size
	}
	return &Overlay{parts: parts, size: outputSize}, nil
}

func overlayLaunchers(central []byte, entries uint16, selectedEntry string) ([]launcherFile, error) {
	if selectedEntry == "" {
		return []launcherFile{{name: "DOSBOX.BAT", contents: []byte("@ECHO OFF\r\nZ:\\PUREMENU\r\n")}}, nil
	}
	dosPath, err := resolveDOSPath(central, entries, selectedEntry)
	if err != nil {
		return nil, err
	}
	if strings.IndexFunc(dosPath, func(value rune) bool { return value >= 0x80 }) >= 0 {
		return nil, ErrInvalid
	}
	return []launcherFile{{name: "AUTOBOOT.DBP", contents: []byte(`C:\` + dosPath)}}, nil
}

func encodeLauncherRecords(launchers []launcherFile) ([]byte, []byte, error) {
	local, central := make([]byte, 0, 256), make([]byte, 0, 256)
	for _, launcher := range launchers {
		launcherLocal, launcherCentral, ok := launcherRecords(launcher.name, launcher.contents)
		localOffset, offsetOK := checkedUint32(len(local))
		if !ok || !offsetOK {
			return nil, nil, ErrInvalid
		}
		binary.LittleEndian.PutUint32(launcherCentral[42:46], localOffset)
		local = append(local, launcherLocal...)
		central = append(central, launcherCentral...)
	}
	return local, central, nil
}

func parseCentralDirectory(central []byte, entries uint16) ([]archiveEntry, error) {
	parsed := make([]archiveEntry, 0, entries)
	offset := 0
	for range entries {
		if offset+46 > len(central) || binary.LittleEndian.Uint32(central[offset:offset+4]) != centralHeaderSignature {
			return nil, ErrInvalid
		}
		nameLength := int(binary.LittleEndian.Uint16(central[offset+28 : offset+30]))
		extraLength := int(binary.LittleEndian.Uint16(central[offset+30 : offset+32]))
		commentLength := int(binary.LittleEndian.Uint16(central[offset+32 : offset+34]))
		next := offset + 46 + nameLength + extraLength + commentLength
		if next > len(central) {
			return nil, ErrInvalid
		}
		rawName := string(central[offset+46 : offset+46+nameLength])
		nonUTF8 := binary.LittleEndian.Uint16(central[offset+8:offset+10])&(1<<11) == 0
		decodedName, err := zipentry.DecodeName(rawName, nonUTF8)
		if err != nil {
			return nil, ErrInvalid
		}
		parsed = append(parsed, archiveEntry{
			start: offset, next: next, rawName: rawName, decodedName: decodedName,
			localOffset: binary.LittleEndian.Uint32(central[offset+42 : offset+46]),
		})
		offset = next
	}
	if offset != len(central) {
		return nil, ErrInvalid
	}
	return parsed, nil
}

func planLegacyNameRewrites(entries []archiveEntry, selectedEntry string) (map[uint32]string, string, error) {
	if selectedEntry == "" {
		return nil, "", nil
	}
	selectedEntry = strings.ReplaceAll(selectedEntry, `\`, "/")
	selected, ok := findSelectedEntry(entries, selectedEntry)
	if !ok {
		return nil, "", ErrInvalid
	}
	rawSelected := strings.TrimSuffix(selected.rawName, "/")
	rawSegments := strings.Split(rawSelected, "/")
	decodedSegments := strings.Split(strings.TrimSuffix(selected.decodedName, "/"), "/")
	if len(rawSegments) != len(decodedSegments) {
		return nil, "", ErrInvalid
	}
	replacements, err := planSelectedPathReplacements(entries, rawSegments)
	if err != nil {
		return nil, "", err
	}
	mappedNames, err := applyLegacyNameRewrites(entries, replacements)
	if err != nil {
		return nil, "", err
	}
	mappedSelected, err := rewriteLegacyPath(rawSelected, replacements)
	if err != nil || strings.IndexFunc(mappedSelected, func(value rune) bool { return value >= 0x80 }) >= 0 {
		return nil, "", ErrInvalid
	}
	return mappedNames, mappedSelected, nil
}

func findSelectedEntry(entries []archiveEntry, selectedEntry string) (archiveEntry, bool) {
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.decodedName, "/")
		if name == selectedEntry || strings.EqualFold(name, selectedEntry) {
			return entry, true
		}
	}
	return archiveEntry{}, false
}

func planSelectedPathReplacements(entries []archiveEntry, rawSegments []string) (map[string]string, error) {
	replacements := make(map[string]string)
	parent := ""
	for index, segment := range rawSegments {
		path := segment
		if parent != "" {
			path = parent + "/" + segment
		}
		if strings.IndexFunc(segment, func(value rune) bool { return value >= 0x80 }) >= 0 {
			replacement, ok := availableLegacyReplacement(
				entries, parent, index, segment, path, index == len(rawSegments)-1,
			)
			if !ok {
				return nil, ErrInvalid
			}
			replacements[path] = replacement
		}
		parent = path
	}
	return replacements, nil
}

func applyLegacyNameRewrites(
	entries []archiveEntry,
	replacements map[string]string,
) (map[uint32]string, error) {
	mappedNames := make(map[uint32]string)
	for _, entry := range entries {
		mapped, err := rewriteLegacyPath(entry.rawName, replacements)
		if err != nil {
			return nil, err
		}
		if mapped != entry.rawName {
			if _, exists := mappedNames[entry.localOffset]; exists {
				return nil, ErrInvalid
			}
			mappedNames[entry.localOffset] = mapped
		}
	}
	return mappedNames, nil
}

func availableLegacyReplacement(
	entries []archiveEntry,
	parent string,
	depth int,
	original string,
	seed string,
	leaf bool,
) (string, bool) {
	used := make(map[string]struct{})
	for _, entry := range entries {
		trimmed := strings.TrimSuffix(entry.rawName, "/")
		segments := strings.Split(trimmed, "/")
		if len(segments) <= depth {
			continue
		}
		candidateParent := strings.Join(segments[:depth], "/")
		if candidateParent == parent && segments[depth] != original {
			used[strings.ToUpper(segments[depth])] = struct{}{}
		}
	}
	for salt := 0; salt < 256; salt++ {
		replacement := legacyReplacement(seed, salt, leaf)
		if _, exists := used[strings.ToUpper(replacement)]; !exists {
			return replacement, true
		}
	}
	return "", false
}

func legacyReplacement(seed string, salt int, leaf bool) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", seed, salt)))
	base := fmt.Sprintf("RT%06X", digest[:3])
	if !leaf {
		return base
	}
	extension := pathExtension(seed)
	if len(extension) >= 2 && len(extension) <= 4 && dosExtensionSafe(strings.ToUpper(extension[1:])) {
		return base + strings.ToUpper(extension)
	}
	return base
}

func rewriteLegacyPath(rawName string, replacements map[string]string) (string, error) {
	trailingSlash := strings.HasSuffix(rawName, "/")
	trimmed := strings.TrimSuffix(rawName, "/")
	if trimmed == "" {
		return rawName, nil
	}
	segments := strings.Split(trimmed, "/")
	parent := ""
	for index, segment := range segments {
		if segment == "" {
			return "", ErrInvalid
		}
		path := segment
		if parent != "" {
			path = parent + "/" + segment
		}
		if replacement, exists := replacements[path]; exists {
			segments[index] = replacement
		}
		parent = path
	}
	result := strings.Join(segments, "/")
	if trailingSlash {
		result += "/"
	}
	return result, nil
}

func rewriteCentralNames(
	central []byte,
	entries []archiveEntry,
	mappedNames map[uint32]string,
) ([]byte, error) {
	rewritten := make([]byte, 0, len(central))
	for _, entry := range entries {
		name := entry.rawName
		if mapped, exists := mappedNames[entry.localOffset]; exists {
			name = mapped
		}
		nameLength, ok := checkedUint16(len(name))
		if !ok {
			return nil, ErrInvalid
		}
		header := append([]byte(nil), central[entry.start:entry.start+46]...)
		binary.LittleEndian.PutUint16(header[28:30], nameLength)
		originalNameEnd := entry.start + 46 + len(entry.rawName)
		rewritten = append(rewritten, header...)
		rewritten = append(rewritten, name...)
		rewritten = append(rewritten, central[originalNameEnd:entry.next]...)
	}
	return rewritten, nil
}

func rewriteLocalNames(
	source io.ReaderAt,
	centralOffset int64,
	entries []archiveEntry,
	mappedNames map[uint32]string,
) ([]part, int64, map[uint32]uint32, error) {
	sortedEntries := append([]archiveEntry(nil), entries...)
	sort.Slice(sortedEntries, func(left, right int) bool {
		return sortedEntries[left].localOffset < sortedEntries[right].localOffset
	})
	rewrittenOffsets, delta, err := calculateRewrittenOffsets(sortedEntries, mappedNames)
	if err != nil {
		return nil, 0, nil, err
	}
	parts, err := rewriteLocalNameParts(source, centralOffset, sortedEntries, mappedNames)
	if err != nil {
		return nil, 0, nil, err
	}
	return parts, centralOffset + delta, rewrittenOffsets, nil
}

func calculateRewrittenOffsets(
	sortedEntries []archiveEntry,
	mappedNames map[uint32]string,
) (map[uint32]uint32, int64, error) {
	rewrittenOffsets := make(map[uint32]uint32, len(sortedEntries))
	delta := int64(0)
	var previousOffset uint32
	for index, entry := range sortedEntries {
		if entry.localOffset == math.MaxUint32 || index != 0 && entry.localOffset == previousOffset {
			return nil, 0, ErrInvalid
		}
		previousOffset = entry.localOffset
		newOffset, ok := checkedInt64Uint32(int64(entry.localOffset) + delta)
		if !ok {
			return nil, 0, ErrInvalid
		}
		rewrittenOffsets[entry.localOffset] = newOffset
		mappedName, changed := mappedNames[entry.localOffset]
		if changed {
			delta += int64(len(mappedName) - len(entry.rawName))
		}
	}
	return rewrittenOffsets, delta, nil
}

func rewriteLocalNameParts(
	source io.ReaderAt,
	centralOffset int64,
	sortedEntries []archiveEntry,
	mappedNames map[uint32]string,
) ([]part, error) {
	parts := make([]part, 0, len(mappedNames)*3+1)
	cursor := int64(0)
	for _, entry := range sortedEntries {
		mappedName, changed := mappedNames[entry.localOffset]
		if !changed {
			continue
		}
		localOffset := int64(entry.localOffset)
		if localOffset < cursor || localOffset+30+int64(len(entry.rawName)) > centralOffset {
			return nil, ErrInvalid
		}
		header, err := rewriteLocalHeader(source, entry, mappedName)
		if err != nil {
			return nil, err
		}
		if localOffset > cursor {
			parts = append(parts, part{source: source, start: cursor, size: localOffset - cursor})
		}
		parts = append(parts,
			part{bytes: header, size: int64(len(header))},
			part{bytes: []byte(mappedName), size: int64(len(mappedName))},
		)
		cursor = localOffset + 30 + int64(len(entry.rawName))
	}
	if cursor > centralOffset {
		return nil, ErrInvalid
	}
	if cursor < centralOffset {
		parts = append(parts, part{source: source, start: cursor, size: centralOffset - cursor})
	}
	return parts, nil
}

func rewriteLocalHeader(source io.ReaderAt, entry archiveEntry, mappedName string) ([]byte, error) {
	localOffset := int64(entry.localOffset)
	header := make([]byte, 30)
	if _, err := source.ReadAt(header, localOffset); err != nil ||
		binary.LittleEndian.Uint32(header[:4]) != localHeaderSignature ||
		int(binary.LittleEndian.Uint16(header[26:28])) != len(entry.rawName) {
		return nil, ErrInvalid
	}
	originalName := make([]byte, len(entry.rawName))
	_, err := source.ReadAt(originalName, localOffset+30)
	if err != nil || !bytes.Equal(originalName, []byte(entry.rawName)) {
		return nil, ErrInvalid
	}
	mappedLength, ok := checkedUint16(len(mappedName))
	if !ok {
		return nil, ErrInvalid
	}
	binary.LittleEndian.PutUint16(header[26:28], mappedLength)
	return header, nil
}

//nolint:funlen,gocognit,gocyclo // Mirrors DOSBox Pure's ordered directory, 8.3, and collision state machine.
func resolveDOSPath(
	central []byte,
	entries uint16,
	selectedEntry string,
) (string, error) {
	selectedEntry = strings.ReplaceAll(selectedEntry, `\`, "/")
	directories := map[string]string{"": ""}
	used := map[string]map[string]struct{}{"": {"AUTOBOOT.DBP": {}}}
	offset := 0
	for range entries {
		if offset+46 > len(central) || binary.LittleEndian.Uint32(central[offset:offset+4]) != centralHeaderSignature {
			return "", ErrInvalid
		}
		nameLength := int(binary.LittleEndian.Uint16(central[offset+28 : offset+30]))
		extraLength := int(binary.LittleEndian.Uint16(central[offset+30 : offset+32]))
		commentLength := int(binary.LittleEndian.Uint16(central[offset+32 : offset+34]))
		next := offset + 46 + nameLength + extraLength + commentLength
		if next > len(central) {
			return "", ErrInvalid
		}
		rawOriginal := string(central[offset+46 : offset+46+nameLength])
		nonUTF8 := binary.LittleEndian.Uint16(central[offset+8:offset+10])&(1<<11) == 0
		original, decodeErr := zipentry.DecodeName(rawOriginal, nonUTF8)
		if decodeErr != nil {
			return "", ErrInvalid
		}
		offset = next
		trimmed := strings.TrimSuffix(original, "/")
		rawTrimmed := strings.TrimSuffix(rawOriginal, "/")
		if trimmed == "" || (!strings.Contains(trimmed, "/") && isReservedLauncher(trimmed)) {
			continue
		}
		segments := strings.Split(trimmed, "/")
		rawSegments := strings.Split(rawTrimmed, "/")
		if len(rawSegments) != len(segments) {
			return "", ErrInvalid
		}
		originalParent, dosParent := "", ""
		for index, segment := range segments {
			rawSegment := rawSegments[index]
			if segment == "" || rawSegment == "" {
				return "", ErrInvalid
			}
			originalPath := segment
			if originalParent != "" {
				originalPath = originalParent + "/" + segment
			}
			isDirectory := index < len(segments)-1 || strings.HasSuffix(original, "/")
			if isDirectory {
				if mapped, exists := directories[originalPath]; exists {
					dosParent, originalParent = mapped, originalPath
					continue
				}
			}
			// DOSBox Pure derives its 8.3 alias from the bytes stored in the ZIP,
			// while Retrom addresses the member by the normalized decoded path.
			alias := make8Dot3(rawSegment)
			parentUsed := used[dosParent]
			if parentUsed == nil {
				parentUsed = map[string]struct{}{}
				used[dosParent] = parentUsed
			}
			for {
				if _, exists := parentUsed[strings.ToUpper(alias)]; !exists {
					break
				}
				var ok bool
				alias, ok = increment8Dot3(alias)
				if !ok {
					return "", ErrInvalid
				}
			}
			parentUsed[strings.ToUpper(alias)] = struct{}{}
			dosPath := alias
			if dosParent != "" {
				dosPath = dosParent + `\` + alias
			}
			if isDirectory {
				directories[originalPath] = dosPath
				used[dosPath] = map[string]struct{}{}
				dosParent, originalParent = dosPath, originalPath
				continue
			}
			if originalPath == selectedEntry || strings.EqualFold(originalPath, selectedEntry) {
				return dosPath, nil
			}
		}
	}
	if offset != len(central) {
		return "", ErrInvalid
	}
	return "", ErrInvalid
}

func make8Dot3(source string) string {
	dot := strings.LastIndex(source, ".")
	if dot <= 0 {
		dot = len(source)
	}
	base, extension := source[:dot], ""
	if dot < len(source) {
		extension = source[dot+1:]
	}
	unchanged := len(base) <= 8 && len(extension) <= 3
	if unchanged {
		for index, value := range []byte(source) {
			if value != '.' || index != dot {
				if !validDOSByte(value) {
					unchanged = false
					break
				}
			}
		}
	}
	if unchanged {
		return source
	}
	baseLeft, baseRight := len(base), 0
	if len(base) > 8 {
		baseLeft, baseRight = 4, 4
	}
	result := filterDOSBytes(base[:baseLeft])
	if baseRight != 0 {
		result += filterDOSBytes(base[len(base)-baseRight:])
	}
	if result == "" {
		result = "-"
	}
	if extension != "" {
		result += "." + filterDOSBytes(extension[:min(len(extension), 3)])
	}
	return result
}

func filterDOSBytes(source string) string {
	result := make([]byte, len(source))
	for index, value := range []byte(source) {
		switch {
		case value >= 'a' && value <= 'z':
			result[index] = value - ('a' - 'A')
		case validDOSByte(value):
			result[index] = value
		case value >= 0x80:
			result[index] = dosUpperByte(value)
		default:
			result[index] = '-'
		}
	}
	return string(result)
}

func validDOSByte(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' ||
		strings.ContainsRune("!#$%&'()+-@^_`{}~", rune(value)) ||
		value == 0x80 || value >= 0x8e && value <= 0x90 || value == 0x92 ||
		value >= 0x99 && value <= 0x9f || value >= 0xa5
}

// dosUpperByte reproduces DOSBox Pure's built-in CP437 upper-case mapping for
// the high bytes that its 8.3 validity table requires it to normalize.
func dosUpperByte(value byte) byte {
	switch value {
	case 0x81:
		return 0x9a
	case 0x82, 0x88, 0x89, 0x8a:
		return 'E'
	case 0x83, 0x85, 0xa0:
		return 'A'
	case 0x84:
		return 0x8e
	case 0x86:
		return 0x8f
	case 0x87:
		return 0x80
	case 0x8b, 0x8c, 0x8d, 0xa1:
		return 'I'
	case 0x91:
		return 0x92
	case 0x93, 0x95, 0xa2:
		return 'O'
	case 0x94:
		return 0x99
	case 0x96, 0x97, 0xa3:
		return 'U'
	case 0x98:
		return 'Y'
	case 0xa4:
		return 0xa5
	default:
		return value
	}
}

func increment8Dot3(source string) (string, bool) {
	name := []byte(source)
	dot := strings.IndexByte(source, '.')
	baseLength := dot
	if dot == -1 {
		baseLength = len(name)
	}
	index := baseLength / 2
	for offset := 0; offset < 3; offset++ {
		candidate := index + offset
		if candidate < baseLength && name[candidate] < '~' {
			name[candidate]++
			return string(name), true
		}
	}
	return "", false
}

func isReservedLauncher(name string) bool {
	return strings.EqualFold(name, "DOSBOX.BAT") || strings.EqualFold(name, "AUTOBOOT.DBP")
}

func readEnd(source io.ReaderAt, size int64) (int64, []byte, error) {
	tailSize := min(size, int64(endMinimumSize+maximumCommentSize))
	tail := make([]byte, tailSize)
	if _, err := source.ReadAt(tail, size-tailSize); err != nil {
		return 0, nil, ErrInvalid
	}
	for index := len(tail) - endMinimumSize; index >= 0; index-- {
		if binary.LittleEndian.Uint32(tail[index:index+4]) != endSignature {
			continue
		}
		commentLength := int(binary.LittleEndian.Uint16(tail[index+20 : index+22]))
		if index+endMinimumSize+commentLength != len(tail) {
			continue
		}
		return size - tailSize + int64(index), append([]byte(nil), tail[index:]...), nil
	}
	return 0, nil, ErrInvalid
}

func patchCentralDirectory(
	central []byte,
	entries []archiveEntry,
	mappedNames map[uint32]string,
	rewrittenOffsets map[uint32]uint32,
	offsetDelta uint32,
) ([]byte, uint16, error) {
	patched := make([]byte, 0, len(central))
	var retainedEntries uint16
	for _, entry := range entries {
		rewrittenOffset, exists := rewrittenOffsets[entry.localOffset]
		if !exists || rewrittenOffset > math.MaxUint32-offsetDelta {
			return nil, 0, ErrInvalid
		}
		name := entry.rawName
		if mapped, mappedExists := mappedNames[entry.localOffset]; mappedExists {
			name = mapped
		}
		nameLength, ok := checkedUint16(len(name))
		if !ok {
			return nil, 0, ErrInvalid
		}
		if !isReservedLauncher(entry.rawName) {
			header := append([]byte(nil), central[entry.start:entry.start+46]...)
			binary.LittleEndian.PutUint16(header[28:30], nameLength)
			binary.LittleEndian.PutUint32(header[42:46], rewrittenOffset+offsetDelta)
			originalNameEnd := entry.start + 46 + len(entry.rawName)
			patched = append(patched, header...)
			patched = append(patched, name...)
			patched = append(patched, central[originalNameEnd:entry.next]...)
			retainedEntries++
		}
	}
	return patched, retainedEntries, nil
}

func dosExtensionSafe(value string) bool {
	for _, current := range []byte(value) {
		if current >= 'A' && current <= 'Z' || current >= '0' && current <= '9' || current == '_' || current == '-' {
			continue
		}
		return false
	}
	return value != ""
}

func pathExtension(value string) string {
	if dot := strings.LastIndexByte(value, '.'); dot >= 0 {
		return value[dot:]
	}
	return ""
}

func launcherRecords(name string, contents []byte) ([]byte, []byte, bool) {
	nameLength, ok := checkedUint16(len(name))
	if !ok {
		return nil, nil, false
	}
	checksum := crc32.ChecksumIEEE(contents)
	local := make([]byte, 30+len(name)+len(contents))
	binary.LittleEndian.PutUint32(local[0:4], localHeaderSignature)
	binary.LittleEndian.PutUint16(local[4:6], 20)
	binary.LittleEndian.PutUint16(local[8:10], 0)
	binary.LittleEndian.PutUint16(local[10:12], 0)
	binary.LittleEndian.PutUint16(local[12:14], 33)
	binary.LittleEndian.PutUint32(local[14:18], checksum)
	contentsSize, _ := checkedUint32(len(contents))
	binary.LittleEndian.PutUint32(local[18:22], contentsSize)
	binary.LittleEndian.PutUint32(local[22:26], contentsSize)
	binary.LittleEndian.PutUint16(local[26:28], nameLength)
	copy(local[30:], name)
	copy(local[30+len(name):], contents)
	central := make([]byte, 46+len(name))
	binary.LittleEndian.PutUint32(central[0:4], centralHeaderSignature)
	binary.LittleEndian.PutUint16(central[4:6], 20)
	binary.LittleEndian.PutUint16(central[6:8], 20)
	binary.LittleEndian.PutUint16(central[10:12], 0)
	binary.LittleEndian.PutUint16(central[12:14], 0)
	binary.LittleEndian.PutUint16(central[14:16], 33)
	binary.LittleEndian.PutUint32(central[16:20], checksum)
	binary.LittleEndian.PutUint32(central[20:24], contentsSize)
	binary.LittleEndian.PutUint32(central[24:28], contentsSize)
	binary.LittleEndian.PutUint16(central[28:30], nameLength)
	binary.LittleEndian.PutUint32(central[38:42], uint32(0o100644)<<16)
	copy(central[46:], name)
	return local, central, true
}

func checkedUint16(value int) (uint16, bool) {
	if value < 0 || uint64(value) > math.MaxUint16 {
		return 0, false
	}
	return uint16(value), true
}

func checkedUint32(value int) (uint32, bool) {
	if value < 0 || uint64(value) > math.MaxUint32 {
		return 0, false
	}
	return uint32(value), true
}

func checkedInt64Uint32(value int64) (uint32, bool) {
	if value < 0 || value > math.MaxUint32 {
		return 0, false
	}
	return uint32(value), true
}

func (overlay *Overlay) Size() int64 { return overlay.size }

func (overlay *Overlay) Read(destination []byte) (int, error) {
	written, err := overlay.ReadAt(destination, overlay.offset)
	overlay.offset += int64(written)
	return written, err
}

func (overlay *Overlay) ReadAt(destination []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, ErrInvalid
	}
	if offset >= overlay.size {
		return 0, io.EOF
	}
	written := 0
	for written < len(destination) && offset < overlay.size {
		part, partOffset := overlay.partAt(offset)
		limit := min(int64(len(destination)-written), part.size-partOffset)
		var count int
		var err error
		if part.bytes != nil {
			count = copy(destination[written:written+int(limit)], part.bytes[partOffset:partOffset+limit])
		} else {
			count, err = part.source.ReadAt(destination[written:written+int(limit)], part.start+partOffset)
		}
		written += count
		offset += int64(count)
		if err != nil && !errors.Is(err, io.EOF) {
			return written, fmt.Errorf("read source archive: %w", err)
		}
		if int64(count) != limit {
			return written, io.ErrUnexpectedEOF
		}
	}
	if written < len(destination) {
		return written, io.EOF
	}
	return written, nil
}

func (overlay *Overlay) Seek(offset int64, whence int) (int64, error) {
	next := offset
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		next += overlay.offset
	case io.SeekEnd:
		next += overlay.size
	default:
		return 0, ErrInvalid
	}
	if next < 0 {
		return 0, ErrInvalid
	}
	overlay.offset = next
	return next, nil
}

func (overlay *Overlay) partAt(offset int64) (part, int64) {
	for _, candidate := range overlay.parts {
		if offset < candidate.size {
			return candidate, offset
		}
		offset -= candidate.size
	}
	return part{}, 0
}

var (
	_ io.ReadSeeker = (*Overlay)(nil)
	_ io.ReaderAt   = (*Overlay)(nil)
)
