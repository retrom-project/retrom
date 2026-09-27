package arcadedat

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Catalog struct {
	Stats    Stats
	Machines []Machine
}

type Machine struct {
	Name           string
	Description    string
	Year           string
	Manufacturer   string
	CloneOf        string
	ROMOf          string
	ExplicitBIOS   bool
	Classification string
	BIOSSets       []BIOSSet
	ROMs           []ROMEntry
	Disks          []DiskEntry
}

type BIOSSet struct {
	Name        string
	Description string
	Default     bool
}

type ROMEntry struct {
	Ordinal   int
	Name      string
	SizeBytes int64
	CRC32     string
	SHA1      string
	Status    string
	MergeName string
	BIOSName  string
}

type DiskEntry struct {
	Ordinal int
	Name    string
	SHA1    string
	Status  string
}

// ParseCatalog uses the same bounded, non-resolving validator as Parse before
// materializing the index rows consumed by matching and dependency planning.
func ParseCatalog(ctx context.Context, source io.Reader, coreID string) (Catalog, error) {
	contents, err := io.ReadAll(io.LimitReader(source, maxDATBytes+1))
	if err != nil {
		return Catalog{}, fmt.Errorf("arcadedat/catalog: %w", err)
	}
	if len(contents) > maxDATBytes {
		return Catalog{}, ErrLimitExceeded
	}
	stats, err := Parse(ctx, bytes.NewReader(contents), coreID)
	if err != nil {
		return Catalog{}, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(contents))
	decoder.Strict = true
	catalog := Catalog{Stats: stats, Machines: make([]Machine, 0, stats.MachineCount)}
	state := catalogParseState{catalog: &catalog}
	for {
		if err := ctx.Err(); err != nil {
			return Catalog{}, fmt.Errorf("arcadedat/catalog: %w", err)
		}
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Catalog{}, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
		}
		if err := state.consume(token); err != nil {
			return Catalog{}, err
		}
	}
	applyCatalogClassifications(catalog.Machines)
	return catalog, nil
}

type catalogParseState struct {
	catalog   *Catalog
	current   *Machine
	textField string
	text      strings.Builder
}

func (state *catalogParseState) consume(token xml.Token) error {
	switch value := token.(type) {
	case xml.StartElement:
		state.start(value)
	case xml.CharData:
		if state.textField != "" {
			_, _ = state.text.Write(value)
		}
	case xml.EndElement:
		return state.end(value)
	}
	return nil
}

func (state *catalogParseState) start(element xml.StartElement) {
	switch element.Name.Local {
	case "machine", "game":
		state.catalog.Machines = append(state.catalog.Machines, Machine{
			Name:           attribute(element, "name"),
			CloneOf:        attribute(element, "cloneof"),
			ROMOf:          attribute(element, "romof"),
			ExplicitBIOS:   attribute(element, "isbios") == "yes",
			Classification: "NORMAL",
		})
		state.current = &state.catalog.Machines[len(state.catalog.Machines)-1]
	case "description", "year", "manufacturer":
		state.startText(element.Name.Local)
	case "biosset":
		state.appendBIOS(element)
	case "rom":
		state.appendROM(element)
	case "disk":
		state.appendDisk(element)
	}
}

func (state *catalogParseState) startText(field string) {
	if state.current == nil {
		return
	}
	state.textField = field
	state.text.Reset()
}

func (state *catalogParseState) appendBIOS(element xml.StartElement) {
	if state.current == nil {
		return
	}
	state.current.BIOSSets = append(state.current.BIOSSets, BIOSSet{
		Name:        attribute(element, "name"),
		Description: attribute(element, "description"),
		Default:     attribute(element, "default") == "yes",
	})
}

func (state *catalogParseState) appendROM(element xml.StartElement) {
	if state.current == nil {
		return
	}
	size, _ := strconv.ParseInt(zeroIfEmpty(attribute(element, "size")), 10, 64)
	state.current.ROMs = append(state.current.ROMs, ROMEntry{
		Ordinal:   len(state.current.ROMs),
		Name:      attribute(element, "name"),
		SizeBytes: size,
		CRC32:     strings.ToLower(attribute(element, "crc")),
		SHA1:      strings.ToLower(attribute(element, "sha1")),
		Status:    catalogStatus(attribute(element, "status")),
		MergeName: attribute(element, "merge"),
		BIOSName:  attribute(element, "bios"),
	})
}

func (state *catalogParseState) appendDisk(element xml.StartElement) {
	if state.current == nil {
		return
	}
	state.current.Disks = append(state.current.Disks, DiskEntry{
		Ordinal: len(state.current.Disks),
		Name:    attribute(element, "name"),
		SHA1:    strings.ToLower(attribute(element, "sha1")),
		Status:  catalogStatus(attribute(element, "status")),
	})
}

func (state *catalogParseState) end(element xml.EndElement) error {
	if state.textField != "" && element.Name.Local == state.textField && state.current != nil {
		if err := state.finishText(); err != nil {
			return err
		}
	}
	if (element.Name.Local == "machine" || element.Name.Local == "game") && state.current != nil {
		if err := validateMachine(*state.current); err != nil {
			return err
		}
		state.current = nil
	}
	return nil
}

func (state *catalogParseState) finishText() error {
	field := strings.TrimSpace(state.text.String())
	if !utf8.ValidString(field) || len(field) > maxFieldBytes {
		return ErrLimitExceeded
	}
	switch state.textField {
	case "description":
		state.current.Description = field
	case "year":
		state.current.Year = field
	case "manufacturer":
		state.current.Manufacturer = field
	}
	state.textField = ""
	return nil
}

func applyCatalogClassifications(machines []Machine) {
	classifications := make(map[string]string)
	for _, machine := range machines {
		if machine.ExplicitBIOS {
			classifications[machine.Name] = "EXPLICIT_BIOS"
		}
		if machine.ROMOf != "" && machine.ROMOf != machine.CloneOf {
			if _, exists := classifications[machine.ROMOf]; !exists {
				classifications[machine.ROMOf] = "ROMOF_INFERENCE"
			}
		}
	}
	for index := range machines {
		if classification := classifications[machines[index].Name]; classification != "" {
			machines[index].Classification = classification
		}
	}
}

func validateMachine(machine Machine) error {
	biosNames := make(map[string]struct{}, len(machine.BIOSSets))
	defaults := 0
	for _, bios := range machine.BIOSSets {
		if !validName(bios.Name) {
			return fmt.Errorf("%w: bios name", ErrInvalidDocument)
		}
		if _, duplicate := biosNames[bios.Name]; duplicate {
			return fmt.Errorf("%w: duplicate bios", ErrInvalidDocument)
		}
		biosNames[bios.Name] = struct{}{}
		if bios.Default {
			defaults++
		}
	}
	if len(machine.BIOSSets) > 0 && defaults != 1 {
		return fmt.Errorf("%w: default bios", ErrInvalidDocument)
	}
	for _, rom := range machine.ROMs {
		if rom.BIOSName != "" {
			if _, exists := biosNames[rom.BIOSName]; !exists {
				return fmt.Errorf("%w: unknown rom bios", ErrInvalidDocument)
			}
		}
	}
	return nil
}

func zeroIfEmpty(value string) string {
	if value == "" {
		return "0"
	}
	return value
}

func catalogStatus(value string) string {
	switch normalizedStatus(value) {
	case "nodump":
		return "NODUMP"
	case "baddump":
		return "BADDUMP"
	default:
		return "GOOD"
	}
}
