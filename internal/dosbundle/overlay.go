package dosbundle

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"strings"
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
	launcherName := "DOSBOX.BAT"
	launcherContents := []byte("@ECHO OFF\r\nZ:\\PUREMENU\r\n")
	if selectedEntry != "" {
		dosPath, err := resolveDOSPath(central, entries, selectedEntry)
		if err != nil {
			return nil, err
		}
		launcherName = "AUTOBOOT.DBP"
		launcherContents = []byte(`C:\` + dosPath)
	}
	local, centralEntry, ok := launcherRecords(launcherName, launcherContents)
	if !ok {
		return nil, ErrInvalid
	}
	offsetDelta, ok := checkedUint32(len(local))
	if !ok {
		return nil, ErrInvalid
	}
	central, retainedEntries, err := patchCentralDirectory(central, entries, offsetDelta)
	if err != nil {
		return nil, err
	}
	commentLength := int(binary.LittleEndian.Uint16(end[20:22]))
	if int64(endMinimumSize+commentLength) != size-endOffset {
		return nil, ErrInvalid
	}
	outputEnd := append([]byte(nil), end...)
	binary.LittleEndian.PutUint16(outputEnd[8:10], retainedEntries+1)
	binary.LittleEndian.PutUint16(outputEnd[10:12], retainedEntries+1)
	newCentralSize := len(centralEntry) + len(central)
	newCentralOffset := int64(len(local)) + centralOffset
	if newCentralSize > math.MaxUint32 || newCentralOffset > math.MaxUint32 {
		return nil, ErrInvalid
	}
	binary.LittleEndian.PutUint32(outputEnd[12:16], uint32(newCentralSize))
	binary.LittleEndian.PutUint32(outputEnd[16:20], uint32(newCentralOffset))
	parts := []part{
		{bytes: local, size: int64(len(local))},
		{source: source, start: 0, size: centralOffset},
		{bytes: centralEntry, size: int64(len(centralEntry))},
		{bytes: central, size: int64(len(central))},
		{bytes: outputEnd, size: int64(len(outputEnd))},
	}
	var outputSize int64
	for _, outputPart := range parts {
		outputSize += outputPart.size
	}
	return &Overlay{parts: parts, size: outputSize}, nil
}

//nolint:funlen,gocognit,gocyclo // Mirrors DOSBox Pure's ordered directory, 8.3, and collision state machine.
func resolveDOSPath(central []byte, entries uint16, selectedEntry string) (string, error) {
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
		original := strings.ReplaceAll(string(central[offset+46:offset+46+nameLength]), `\`, "/")
		offset = next
		trimmed := strings.TrimSuffix(original, "/")
		if trimmed == "" || (!strings.Contains(trimmed, "/") && isReservedLauncher(trimmed)) {
			continue
		}
		segments := strings.Split(trimmed, "/")
		originalParent, dosParent := "", ""
		for index, segment := range segments {
			if segment == "" {
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
			alias := make8Dot3(segment)
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
		default:
			result[index] = '-'
		}
	}
	return string(result)
}

func validDOSByte(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' ||
		strings.ContainsRune("!#$%&'()+-@^_`{}~", rune(value))
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

func patchCentralDirectory(central []byte, entries uint16, offsetDelta uint32) ([]byte, uint16, error) {
	patched := make([]byte, 0, len(central))
	var retainedEntries uint16
	offset := 0
	for range entries {
		if offset+46 > len(central) || binary.LittleEndian.Uint32(central[offset:offset+4]) != centralHeaderSignature {
			return nil, 0, ErrInvalid
		}
		nameLength := int(binary.LittleEndian.Uint16(central[offset+28 : offset+30]))
		extraLength := int(binary.LittleEndian.Uint16(central[offset+30 : offset+32]))
		commentLength := int(binary.LittleEndian.Uint16(central[offset+32 : offset+34]))
		next := offset + 46 + nameLength + extraLength + commentLength
		if next > len(central) {
			return nil, 0, ErrInvalid
		}
		originalOffset := binary.LittleEndian.Uint32(central[offset+42 : offset+46])
		if originalOffset == math.MaxUint32 || originalOffset > math.MaxUint32-offsetDelta {
			return nil, 0, ErrInvalid
		}
		binary.LittleEndian.PutUint32(central[offset+42:offset+46], originalOffset+offsetDelta)
		name := string(central[offset+46 : offset+46+nameLength])
		if !isReservedLauncher(name) {
			patched = append(patched, central[offset:next]...)
			retainedEntries++
		}
		offset = next
	}
	if offset != len(central) {
		return nil, 0, ErrInvalid
	}
	return patched, retainedEntries, nil
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
