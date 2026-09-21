package arcadedat

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxDATBytes      = 64 << 20
	maxDirectiveSize = 64 << 10
	maxElements      = 500000
	maxDepth         = 32
	maxAttributes    = 64
	maxNameBytes     = 255
	maxFieldBytes    = 4096
)

var (
	ErrUnsafeDTD       = errors.New("DAT_UNSAFE_DTD")
	ErrLimitExceeded   = errors.New("DAT_LIMIT_EXCEEDED")
	ErrInvalidDocument = errors.New("DAT_INVALID_DOCUMENT")
)

type Stats struct {
	MachineCount                    int `json:"machine_count"`
	ROMEntryCount                   int `json:"rom_entry_count"`
	ROMEntryWithMergeCount          int `json:"rom_entry_with_merge_count"`
	ROMEntryWithBIOSCount           int `json:"rom_entry_with_bios_count"`
	ROMNodumpCount                  int `json:"rom_nodump_count"`
	ROMBaddumpCount                 int `json:"rom_baddump_count"`
	ROMMissingCRC32Count            int `json:"rom_missing_crc32_count"`
	ROMMissingSHA1Count             int `json:"rom_missing_sha1_count"`
	ROMMissingAllHashCount          int `json:"rom_missing_all_hash_count"`
	NonNodumpROMMissingAllHashCount int `json:"non_nodump_rom_missing_all_hash_count"`
	BIOSSetCount                    int `json:"bios_set_count"`
	DefaultBIOSSetCount             int `json:"default_bios_set_count"`
	DiskEntryCount                  int `json:"disk_entry_count"`
	DiskMissingSHA1Count            int `json:"disk_missing_sha1_count"`
	SampleEntryCount                int `json:"sample_entry_count"`
	CloneofRelationCount            int `json:"cloneof_relation_count"`
	RomofRelationCount              int `json:"romof_relation_count"`
	ExplicitBIOSMachineCount        int `json:"explicit_bios_machine_count"`
	BaseDependencyTargetCount       int `json:"base_dependency_target_count"`
	UnresolvedCloneofTargetCount    int `json:"unresolved_cloneof_target_count"`
	UnresolvedRomofTargetCount      int `json:"unresolved_romof_target_count"`
}

type machineInfo struct {
	cloneof string
	romof   string
	bios    bool
}

func Parse(ctx context.Context, source io.Reader, coreID string) (Stats, error) {
	family, supported := FamilyForCore(coreID)
	if !supported {
		return Stats{}, fmt.Errorf("%w: unsupported core", ErrInvalidDocument)
	}
	limited := &io.LimitedReader{R: source, N: maxDATBytes + 1}
	decoder := xml.NewDecoder(limited)
	decoder.Strict = true
	state := datParseState{
		family:      family,
		machines:    make(map[string]machineInfo),
		baseTargets: make(map[string]struct{}),
	}
	for {
		if err := ctx.Err(); err != nil {
			return Stats{}, fmt.Errorf("arcadedat/parser: %w", err)
		}
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Stats{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
		}
		if limited.N <= 0 {
			return Stats{}, ErrLimitExceeded
		}
		if err := state.consume(token); err != nil {
			return Stats{}, err
		}
	}
	if !state.rootSeen || state.depth != 0 {
		return Stats{}, ErrInvalidDocument
	}
	state.resolveRelations()
	return state.stats, nil
}

type datParseState struct {
	family         Family
	stats          Stats
	machines       map[string]machineInfo
	baseTargets    map[string]struct{}
	rootSeen       bool
	directiveSeen  bool
	depth          int
	elements       int
	currentMachine string
}

func (state *datParseState) consume(token xml.Token) error {
	switch value := token.(type) {
	case xml.Directive:
		if state.rootSeen || state.directiveSeen || validateDirective(value) != nil {
			return ErrUnsafeDTD
		}
		state.directiveSeen = true
	case xml.ProcInst:
		if value.Target != "xml" || state.rootSeen {
			return ErrUnsafeDTD
		}
	case xml.StartElement:
		return state.start(value)
	case xml.CharData:
		if len(value) > maxFieldBytes {
			return ErrLimitExceeded
		}
	case xml.EndElement:
		return state.end(value)
	}
	return nil
}

func (state *datParseState) start(element xml.StartElement) error {
	state.depth++
	state.elements++
	if state.depth > maxDepth || state.elements > maxElements || len(element.Attr) > maxAttributes {
		return ErrLimitExceeded
	}
	if !state.rootSeen {
		return state.startRoot(element)
	}
	if element.Name.Space != "" {
		return fmt.Errorf("%w: namespace", ErrInvalidDocument)
	}
	switch element.Name.Local {
	case "machine", "game":
		return state.startMachine(element)
	case "rom":
		if state.currentMachine == "" {
			return fmt.Errorf("%w: rom outside machine", ErrInvalidDocument)
		}
		return countROM(element, &state.stats)
	case "disk":
		return state.countDisk(element)
	case "biosset":
		state.stats.BIOSSetCount++
		if attribute(element, "default") == "yes" {
			state.stats.DefaultBIOSSetCount++
		}
	case "sample":
		state.stats.SampleEntryCount++
	}
	return nil
}

func (state *datParseState) startRoot(element xml.StartElement) error {
	expected := "mame"
	if state.family == FamilyLogiqxDatafile {
		expected = "datafile"
	}
	if element.Name.Local != expected || element.Name.Space != "" {
		return fmt.Errorf("%w: root", ErrInvalidDocument)
	}
	state.rootSeen = true
	return nil
}

func (state *datParseState) startMachine(element xml.StartElement) error {
	name := attribute(element, "name")
	if !validName(name) {
		return fmt.Errorf("%w: machine name", ErrInvalidDocument)
	}
	if _, exists := state.machines[name]; exists {
		return fmt.Errorf("%w: duplicate machine", ErrInvalidDocument)
	}
	info := machineInfo{
		cloneof: attribute(element, "cloneof"),
		romof:   attribute(element, "romof"),
		bios:    attribute(element, "isbios") == "yes",
	}
	state.machines[name] = info
	state.currentMachine = name
	state.stats.MachineCount++
	state.recordMachineRelations(name, info)
	return nil
}

func (state *datParseState) recordMachineRelations(name string, info machineInfo) {
	if info.cloneof != "" {
		state.stats.CloneofRelationCount++
	}
	if info.romof != "" {
		state.stats.RomofRelationCount++
	}
	if info.bios {
		state.stats.ExplicitBIOSMachineCount++
		state.baseTargets[name] = struct{}{}
	}
	if info.romof != "" && info.romof != info.cloneof {
		state.baseTargets[info.romof] = struct{}{}
	}
}

func (state *datParseState) countDisk(element xml.StartElement) error {
	state.stats.DiskEntryCount++
	status := normalizedStatus(attribute(element, "status"))
	sha1Value := attribute(element, "sha1")
	if sha1Value == "" {
		state.stats.DiskMissingSHA1Count++
		if status != "nodump" {
			return fmt.Errorf("%w: disk hash", ErrInvalidDocument)
		}
		return nil
	}
	if !validHex(sha1Value, 40) {
		return fmt.Errorf("%w: disk sha1", ErrInvalidDocument)
	}
	return nil
}

func (state *datParseState) end(element xml.EndElement) error {
	if (element.Name.Local == "machine" || element.Name.Local == "game") && state.depth == 2 {
		state.currentMachine = ""
	}
	state.depth--
	if state.depth < 0 {
		return ErrInvalidDocument
	}
	return nil
}

func (state *datParseState) resolveRelations() {
	unresolvedCloneof := make(map[string]struct{})
	unresolvedRomof := make(map[string]struct{})
	for _, info := range state.machines {
		if info.cloneof != "" {
			if _, exists := state.machines[info.cloneof]; !exists {
				unresolvedCloneof[info.cloneof] = struct{}{}
			}
		}
		if info.romof != "" {
			if _, exists := state.machines[info.romof]; !exists {
				unresolvedRomof[info.romof] = struct{}{}
			}
		}
	}
	state.stats.UnresolvedCloneofTargetCount = len(unresolvedCloneof)
	state.stats.UnresolvedRomofTargetCount = len(unresolvedRomof)
	state.stats.BaseDependencyTargetCount = len(state.baseTargets)
}

func validateDirective(value xml.Directive) error {
	if len(value) > maxDirectiveSize {
		return ErrLimitExceeded
	}
	text := strings.TrimSpace(string(value))
	upper := strings.ToUpper(text)
	if !strings.HasPrefix(upper, "DOCTYPE ") || strings.Contains(upper, "<!ENTITY") || strings.Contains(text, "%") {
		return ErrUnsafeDTD
	}
	return nil
}

func attribute(element xml.StartElement, name string) string {
	for _, item := range element.Attr {
		if item.Name.Space == "" && item.Name.Local == name {
			return item.Value
		}
	}
	return ""
}

func countROM(element xml.StartElement, stats *Stats) error {
	name := attribute(element, "name")
	if !validName(name) {
		return fmt.Errorf("%w: rom name", ErrInvalidDocument)
	}
	if rawSize := attribute(element, "size"); rawSize != "" {
		if size, err := strconv.ParseInt(rawSize, 10, 64); err != nil || size < 0 {
			return fmt.Errorf("%w: rom size", ErrInvalidDocument)
		}
	}
	crc := attribute(element, "crc")
	sha1Value := attribute(element, "sha1")
	if crc != "" && !validHex(crc, 8) || sha1Value != "" && !validHex(sha1Value, 40) {
		return fmt.Errorf("%w: rom hash", ErrInvalidDocument)
	}
	status := normalizedStatus(attribute(element, "status"))
	recordROMStats(element, stats, crc, sha1Value, status)
	if crc == "" && sha1Value == "" && status != "nodump" {
		return fmt.Errorf("%w: rom missing hash", ErrInvalidDocument)
	}
	return nil
}

func recordROMStats(element xml.StartElement, stats *Stats, crc, sha1Value, status string) {
	stats.ROMEntryCount++
	if attribute(element, "merge") != "" {
		stats.ROMEntryWithMergeCount++
	}
	if attribute(element, "bios") != "" {
		stats.ROMEntryWithBIOSCount++
	}
	if status == "nodump" {
		stats.ROMNodumpCount++
	}
	if status == "baddump" {
		stats.ROMBaddumpCount++
	}
	if crc == "" {
		stats.ROMMissingCRC32Count++
	}
	if sha1Value == "" {
		stats.ROMMissingSHA1Count++
	}
	if crc == "" && sha1Value == "" {
		stats.ROMMissingAllHashCount++
		if status != "nodump" {
			stats.NonNodumpROMMissingAllHashCount++
		}
	}
}

func validName(value string) bool {
	return value != "" && len(value) <= maxNameBytes && utf8.ValidString(value)
}

func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		isDigit := character >= '0' && character <= '9'
		isLower := character >= 'a' && character <= 'f'
		isUpper := character >= 'A' && character <= 'F'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return true
}

func normalizedStatus(value string) string {
	if value == "" {
		return "good"
	}
	return strings.ToLower(value)
}
