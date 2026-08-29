package detector

import (
	"bytes"
	"fmt"
)

const (
	maxLDBBytes   = 64 << 20
	maxLMTBytes   = 16 << 20
	maxINIBytes   = 64 << 10
	maxLCFRecords = 1_000_000
)

type lcfReader struct {
	contents []byte
	offset   int
	code     Code
}

func detectRPG2K(files *catalog) ([]evidence, error) {
	hasLDB := files.exists("RPG_RT.ldb")
	hasLMT := files.exists("RPG_RT.lmt")
	if !hasLDB && !hasLMT {
		return nil, nil
	}
	if !hasLDB {
		return nil, newError(CodeLCFInvalid, "RPG_RT.ldb is missing", nil)
	}
	if !hasLMT {
		return nil, newError(CodeLMTInvalid, "RPG_RT.lmt is missing", nil)
	}
	ldb, err := files.read("RPG_RT.ldb", maxLDBBytes, CodeLCFInvalid)
	if err != nil {
		return nil, err
	}
	generation, family, err := parseLDB(ldb)
	if err != nil {
		return nil, err
	}
	lmt, err := files.read("RPG_RT.lmt", maxLMTBytes, CodeLMTInvalid)
	if err != nil {
		return nil, err
	}
	startMapID, err := parseLMT(lmt)
	if err != nil {
		return nil, err
	}
	startMapPath := fmt.Sprintf("Map%04d.lmu", startMapID)
	if !files.exists(startMapPath) {
		return nil, newError(CodeLMTInvalid, "start map file is missing", nil)
	}
	selfContained, err := parseRPGRTINI(files)
	if err != nil {
		return nil, err
	}
	requirements := []Requirement{RequirementRuntimeValidation}
	if !selfContained {
		requirements = append(requirements, RequirementRPG2KRTP)
	}
	markers := []string{files.original("RPG_RT.ldb"), files.original("RPG_RT.lmt"), files.original(startMapPath)}
	if files.exists("RPG_RT.ini") {
		markers = append(markers, files.original("RPG_RT.ini"))
	}
	return []evidence{{
		generation: generation, family: family, selfContained: selfContained,
		markers: markers, requirements: requirements,
	}}, nil
}

func parseLDB(contents []byte) (Generation, string, error) {
	reader := &lcfReader{contents: contents, code: CodeLCFInvalid}
	if err := reader.header("LcfDataBase"); err != nil {
		return "", "", err
	}
	foundSystem := false
	ldbID := uint32(0)
	for reader.offset < len(reader.contents) {
		chunkID, err := reader.integer()
		if err != nil {
			return "", "", err
		}
		if chunkID == 0 {
			if reader.offset != len(reader.contents) {
				return "", "", reader.invalid("bytes follow database terminator")
			}
			break
		}
		chunk, err := reader.chunk()
		if err != nil {
			return "", "", err
		}
		if chunkID == 0x16 {
			if foundSystem {
				return "", "", reader.invalid("duplicate System chunk")
			}
			foundSystem = true
			ldbID, err = parseLDBSystem(chunk)
			if err != nil {
				return "", "", err
			}
		}
	}
	if !foundSystem {
		return "", "", reader.invalid("System chunk is missing")
	}
	switch ldbID {
	case 0:
		return RPG2000, FamilyRPG2K, nil
	case 2003:
		return RPG2003, FamilyRPG2K, nil
	default:
		return "", "", newError(CodeLCFGenerationUnknown, fmt.Sprintf("unsupported ldb_id %d", ldbID), nil)
	}
}

func parseLDBSystem(contents []byte) (uint32, error) {
	reader := &lcfReader{contents: contents, code: CodeLCFInvalid}
	foundID := false
	value := uint32(0)
	terminated := false
	for reader.offset < len(reader.contents) {
		chunkID, err := reader.integer()
		if err != nil {
			return 0, err
		}
		if chunkID == 0 {
			terminated = true
			break
		}
		chunk, err := reader.chunk()
		if err != nil {
			return 0, err
		}
		if chunkID != 0x0a {
			continue
		}
		if foundID {
			return 0, reader.invalid("duplicate ldb_id field")
		}
		foundID = true
		fieldReader := &lcfReader{contents: chunk, code: CodeLCFInvalid}
		value, err = fieldReader.integer()
		if err != nil || fieldReader.offset != len(chunk) {
			return 0, reader.invalid("invalid ldb_id value")
		}
	}
	if !terminated || reader.offset != len(contents) {
		return 0, reader.invalid("System chunk has no exact terminator")
	}
	return value, nil
}

func parseLMT(contents []byte) (uint32, error) {
	reader := &lcfReader{contents: contents, code: CodeLMTInvalid}
	if err := reader.header("LcfMapTree"); err != nil {
		return 0, err
	}
	mapIDs, err := reader.mapRecords()
	if err != nil {
		return 0, err
	}
	orderCount, err := reader.boundedCount("tree order")
	if err != nil {
		return 0, err
	}
	for range orderCount {
		if _, err := reader.integer(); err != nil {
			return 0, err
		}
	}
	if _, err := reader.integer(); err != nil {
		return 0, err
	}
	startMapID, err := reader.startRecord()
	if err != nil {
		return 0, err
	}
	if reader.offset != len(contents) || startMapID == 0 {
		return 0, reader.invalid("invalid map start or trailing bytes")
	}
	if _, exists := mapIDs[startMapID]; !exists {
		return 0, reader.invalid("start map is absent from MapTree")
	}
	return startMapID, nil
}

func (reader *lcfReader) mapRecords() (map[uint32]struct{}, error) {
	count, err := reader.boundedCount("map")
	if err != nil {
		return nil, err
	}
	mapIDs := make(map[uint32]struct{}, count)
	for range count {
		mapID, readErr := reader.integer()
		if readErr != nil {
			return nil, reader.invalid("invalid map ID")
		}
		if _, duplicate := mapIDs[mapID]; duplicate {
			return nil, reader.invalid("duplicate map ID")
		}
		mapIDs[mapID] = struct{}{}
		if err := reader.skipStruct(); err != nil {
			return nil, err
		}
	}
	return mapIDs, nil
}

func (reader *lcfReader) startRecord() (uint32, error) {
	found := false
	startMapID := uint32(0)
	for {
		fieldID, err := reader.integer()
		if err != nil {
			return 0, err
		}
		if fieldID == 0 {
			return startMapID, nil
		}
		field, err := reader.chunk()
		if err != nil {
			return 0, err
		}
		if fieldID == 1 {
			if found {
				return 0, reader.invalid("duplicate party_map_id")
			}
			found = true
			fieldReader := &lcfReader{contents: field, code: CodeLMTInvalid}
			startMapID, err = fieldReader.integer()
			if err != nil || fieldReader.offset != len(field) {
				return 0, reader.invalid("invalid party_map_id")
			}
		}
	}
}

func (reader *lcfReader) skipStruct() error {
	for {
		fieldID, err := reader.integer()
		if err != nil {
			return err
		}
		if fieldID == 0 {
			return nil
		}
		if _, err := reader.chunk(); err != nil {
			return err
		}
	}
}

func (reader *lcfReader) header(expected string) error {
	length, err := reader.integer()
	if err != nil || length != uint32(len(expected)) || int(length) > len(reader.contents)-reader.offset {
		return reader.invalid("invalid LCF header length")
	}
	actual := reader.contents[reader.offset : reader.offset+int(length)]
	reader.offset += int(length)
	if !bytes.Equal(actual, []byte(expected)) {
		return reader.invalid("invalid LCF header")
	}
	return nil
}

func (reader *lcfReader) chunk() ([]byte, error) {
	length, err := reader.integer()
	if err != nil || uint64(length) > uint64(len(reader.contents)-reader.offset) {
		return nil, reader.invalid("chunk exceeds remaining file")
	}
	result := reader.contents[reader.offset : reader.offset+int(length)]
	reader.offset += int(length)
	return result, nil
}

func (reader *lcfReader) boundedCount(kind string) (int, error) {
	count, err := reader.integer()
	if err != nil || count > maxLCFRecords || uint64(count) > uint64(len(reader.contents)-reader.offset) {
		return 0, reader.invalid(kind + " count exceeds remaining file")
	}
	return int(count), nil
}

func (reader *lcfReader) integer() (uint32, error) {
	value := uint32(0)
	for index := 0; index < 5; index++ {
		if reader.offset >= len(reader.contents) {
			return 0, reader.invalid("truncated varint")
		}
		current := reader.contents[reader.offset]
		reader.offset++
		if value > (^uint32(0) >> 7) {
			return 0, reader.invalid("varint overflow")
		}
		value = value<<7 | uint32(current&0x7f)
		if current&0x80 == 0 {
			return value, nil
		}
	}
	return 0, reader.invalid("varint exceeds five bytes")
}

func (reader *lcfReader) invalid(detail string) error {
	return newError(reader.code, detail, nil)
}
