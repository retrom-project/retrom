package importing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	maximumASARHeaderBytes = 64 << 20
	maximumASARPathDepth   = 64
)

type asarIntegrity struct {
	algorithm string
	hash      string
	blockSize int64
	blocks    []string
}

type asarMember struct {
	path       string
	size       int64
	offset     int64
	unpacked   bool
	integrity  *asarIntegrity
	ordinal    int
	outerEntry validatedElectronZIPItem
}

type asarHeaderWalk struct {
	limits   ArchiveLimits
	seenPath map[string]struct{}
	seenFold map[string]struct{}
	members  []asarMember
	nodes    int
	total    int64
}

func readASARHeader(reader io.Reader, archiveSize int64, limits ArchiveLimits) ([]asarMember, int64, error) {
	var prefix [16]byte
	if _, err := io.ReadFull(reader, prefix[:]); err != nil {
		return nil, 0, invalidElectronASAR("truncated pickle header")
	}
	sizePicklePayload := int64(littleEndianUint32(prefix[0:4]))
	headerPickleSize := int64(littleEndianUint32(prefix[4:8]))
	headerPayloadSize := int64(littleEndianUint32(prefix[8:12]))
	jsonSize := int64(littleEndianUint32(prefix[12:16]))
	dataOffset := 8 + headerPickleSize
	padding := headerPayloadSize - 4 - jsonSize
	if sizePicklePayload != 4 || headerPickleSize != headerPayloadSize+4 ||
		headerPickleSize < 8 || headerPickleSize > maximumASARHeaderBytes ||
		headerPickleSize%4 != 0 || jsonSize < 2 || padding < 0 || padding > 3 ||
		dataOffset > archiveSize {
		return nil, 0, invalidElectronASAR("invalid pickle lengths")
	}
	headerBytes := make([]byte, jsonSize)
	if _, err := io.ReadFull(reader, headerBytes); err != nil {
		return nil, 0, invalidElectronASAR("truncated JSON header")
	}
	paddingBytes := make([]byte, padding)
	if _, err := io.ReadFull(reader, paddingBytes); err != nil || !allZero(paddingBytes) {
		return nil, 0, invalidElectronASAR("invalid pickle padding")
	}
	members, err := decodeASARHeader(headerBytes, archiveSize-dataOffset, limits)
	if err != nil {
		return nil, 0, err
	}
	return members, dataOffset, nil
}

func decodeASARHeader(contents []byte, dataSize int64, limits ArchiveLimits) ([]asarMember, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(contents, &root); err != nil || len(root) != 1 {
		return nil, invalidElectronASAR("invalid JSON root")
	}
	files, exists := root["files"]
	if !exists {
		return nil, invalidElectronASAR("missing files root")
	}
	walk := asarHeaderWalk{
		limits: limits, seenPath: make(map[string]struct{}), seenFold: make(map[string]struct{}),
		members: make([]asarMember, 0),
	}
	if err := walk.directory(files, "", 0); err != nil {
		return nil, err
	}
	if len(walk.members) == 0 || len(walk.members) > limits.MaxEntries {
		return nil, ErrArchiveLimitExceeded
	}
	if err := validateASARRanges(walk.members, dataSize); err != nil {
		return nil, err
	}
	sort.Slice(walk.members, func(left, right int) bool {
		return walk.members[left].path < walk.members[right].path
	})
	for index := range walk.members {
		walk.members[index].ordinal = index
	}
	return walk.members, nil
}

func (walk *asarHeaderWalk) directory(raw json.RawMessage, prefix string, depth int) error {
	if depth > maximumASARPathDepth {
		return invalidElectronASAR("directory depth exceeded")
	}
	var files map[string]json.RawMessage
	if err := json.Unmarshal(raw, &files); err != nil || files == nil {
		return invalidElectronASAR("files node is not an object")
	}
	for name, child := range files {
		pathValue, err := walk.path(prefix, name)
		if err != nil {
			return err
		}
		var node map[string]json.RawMessage
		if err := json.Unmarshal(child, &node); err != nil || node == nil {
			return invalidElectronASAR("entry node is not an object")
		}
		if nested, isDirectory := node["files"]; isDirectory {
			if err := validateASARDirectoryNode(node); err != nil {
				return err
			}
			if err := walk.directory(nested, pathValue, depth+1); err != nil {
				return err
			}
			continue
		}
		member, err := decodeASARMember(pathValue, node)
		if err != nil {
			return err
		}
		if err := walk.addMember(member); err != nil {
			return err
		}
	}
	return nil
}

func (walk *asarHeaderWalk) path(prefix, name string) (string, error) {
	walk.nodes++
	if walk.nodes > walk.limits.MaxEntries || name == "" || strings.ContainsAny(name, "/\\") {
		return "", ErrArchiveLimitExceeded
	}
	pathValue := name
	if prefix != "" {
		pathValue = prefix + "/" + name
	}
	validated, err := ValidateLogicalPath(pathValue)
	if err != nil {
		return "", err
	}
	if err := recordArchivePath(walk.seenPath, walk.seenFold, validated, ASCIICaseFold(validated)); err != nil {
		return "", err
	}
	return validated, nil
}

func (walk *asarHeaderWalk) addMember(member asarMember) error {
	if member.size < 0 || member.size > walk.limits.MaxEntryBytes ||
		walk.total > walk.limits.MaxExpandedBytes-member.size {
		return ErrArchiveLimitExceeded
	}
	walk.total += member.size
	walk.members = append(walk.members, member)
	return nil
}

func validateASARDirectoryNode(node map[string]json.RawMessage) error {
	for key := range node {
		if key != "files" {
			return invalidElectronASAR("directory contains file metadata")
		}
	}
	return nil
}

func decodeASARMember(pathValue string, node map[string]json.RawMessage) (asarMember, error) {
	allowed := map[string]bool{
		"size": true, "offset": true, "unpacked": true, "executable": true, "integrity": true,
	}
	for key := range node {
		if !allowed[key] {
			return asarMember{}, invalidElectronASAR("unsupported file metadata")
		}
	}
	var size int64
	if raw, exists := node["size"]; !exists || json.Unmarshal(raw, &size) != nil || size < 0 {
		return asarMember{}, invalidElectronASAR("invalid file size")
	}
	unpacked, err := decodeOptionalBool(node, "unpacked")
	if err != nil {
		return asarMember{}, err
	}
	if _, err := decodeOptionalBool(node, "executable"); err != nil {
		return asarMember{}, err
	}
	member := asarMember{path: pathValue, size: size, unpacked: unpacked}
	if err := decodeASAROffset(node, &member); err != nil {
		return asarMember{}, err
	}
	if raw, exists := node["integrity"]; exists {
		member.integrity, err = decodeASARIntegrity(raw, size)
		if err != nil {
			return asarMember{}, err
		}
	}
	return member, nil
}

func decodeOptionalBool(node map[string]json.RawMessage, name string) (bool, error) {
	raw, exists := node[name]
	if !exists {
		return false, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, invalidElectronASAR("invalid boolean metadata")
	}
	return value, nil
}

func decodeASAROffset(node map[string]json.RawMessage, member *asarMember) error {
	raw, exists := node["offset"]
	if member.unpacked {
		if exists {
			return invalidElectronASAR("unpacked member has an offset")
		}
		return nil
	}
	var value string
	if !exists || json.Unmarshal(raw, &value) != nil || value == "" ||
		len(value) > 19 || value != "0" && value[0] == '0' {
		return invalidElectronASAR("invalid file offset")
	}
	offset, err := strconv.ParseUint(value, 10, 63)
	if err != nil {
		return invalidElectronASAR("invalid file offset")
	}
	member.offset = int64(offset)
	return nil
}

func decodeASARIntegrity(raw json.RawMessage, size int64) (*asarIntegrity, error) {
	var value struct {
		Algorithm string   `json:"algorithm"`
		Hash      string   `json:"hash"`
		BlockSize int64    `json:"blockSize"`
		Blocks    []string `json:"blocks"`
	}
	if err := json.Unmarshal(raw, &value); err != nil || value.Algorithm != "SHA256" ||
		!validSHA256(value.Hash) || value.BlockSize <= 0 {
		return nil, invalidElectronASAR("invalid integrity metadata")
	}
	expectedBlocks := int64(1)
	if size > 0 {
		expectedBlocks = 1 + (size-1)/value.BlockSize
	}
	if expectedBlocks > math.MaxInt || int64(len(value.Blocks)) != expectedBlocks {
		return nil, invalidElectronASAR("invalid integrity block count")
	}
	for _, block := range value.Blocks {
		if !validSHA256(block) {
			return nil, invalidElectronASAR("invalid integrity block")
		}
	}
	return &asarIntegrity{
		algorithm: value.Algorithm, hash: value.Hash, blockSize: value.BlockSize,
		blocks: append([]string(nil), value.Blocks...),
	}, nil
}

func validateASARRanges(members []asarMember, dataSize int64) error {
	packed := make([]asarMember, 0, len(members))
	for _, member := range members {
		if !member.unpacked {
			packed = append(packed, member)
		}
	}
	sort.Slice(packed, func(left, right int) bool {
		if packed[left].offset != packed[right].offset {
			return packed[left].offset < packed[right].offset
		}
		return packed[left].path < packed[right].path
	})
	var previousEnd int64
	for _, member := range packed {
		if member.offset < previousEnd || member.offset > dataSize || member.size > dataSize-member.offset {
			return invalidElectronASAR("overlapping or out-of-bounds file range")
		}
		if member.size > 0 {
			previousEnd = member.offset + member.size
		}
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func allZero(contents []byte) bool {
	for _, value := range contents {
		if value != 0 {
			return false
		}
	}
	return true
}

func littleEndianUint32(contents []byte) uint32 {
	return uint32(contents[0]) | uint32(contents[1])<<8 |
		uint32(contents[2])<<16 | uint32(contents[3])<<24
}

func validateASARIntegrity(member asarMember, content ArchiveContent) error {
	if member.integrity != nil && content.SHA256 != member.integrity.hash {
		return fmt.Errorf("%w: integrity mismatch for %q", ErrElectronASARInvalid, member.path)
	}
	return nil
}
