package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

func generateRGSS(output string, spec rgssSpec, template string) error {
	want := map[int]struct {
		generation string
		suffix     string
		marker     string
	}{
		1: {"RPGXP", ".rxdata", ".rxproj"},
		2: {"RPGVX", ".rvdata", ".rvproj"},
		3: {"RPGVXACE", ".rvdata2", ".rvproj2"},
	}[spec.RGSSVersion]
	if want.generation == "" || spec.Generation != want.generation ||
		!strings.HasSuffix(spec.ScriptsPath, want.suffix) ||
		!strings.HasSuffix(spec.ProjectMarker, want.marker) ||
		spec.Directory == "" || spec.Marker == "" {
		return fmt.Errorf("invalid RGSS fixture spec for %q", spec.Generation)
	}
	script := template
	replacements := map[string]string{
		"{{MARKER}}": spec.Marker,
		"{{RED}}":    strconv.Itoa(int(spec.AccentRGB[0])),
		"{{GREEN}}":  strconv.Itoa(int(spec.AccentRGB[1])),
		"{{BLUE}}":   strconv.Itoa(int(spec.AccentRGB[2])),
	}
	for before, after := range replacements {
		script = strings.ReplaceAll(script, before, after)
	}
	if strings.Contains(script, "{{") || strings.Contains(script, "}}") {
		return errors.New("RGSS template contains an unresolved placeholder")
	}
	root := filepath.ToSlash(filepath.Join(spec.Directory))
	ini := fmt.Sprintf("[Game]\r\nLibrary=RGSS%d Player\r\nScripts=%s\r\nTitle=%s\r\n", spec.RGSSVersion, strings.ReplaceAll(spec.ScriptsPath, "/", "\\"), spec.Marker)
	files := map[string][]byte{
		"Game.ini":                                []byte(ini),
		spec.ProjectMarker:                        {},
		spec.ScriptsPath:                          marshalScript("Retrom Smoke", []byte(script)),
		"Audio/SE/retrom-tone.wav":                toneWAV(),
		"Graphics/Unused/retrom-lazy-padding.bin": make([]byte, 5*1024*1024),
	}
	for name, contents := range files {
		if err := writeFile(output, root+"/"+name, contents); err != nil {
			return err
		}
	}
	return nil
}

func marshalScript(name string, source []byte) []byte {
	var result bytes.Buffer
	result.Write([]byte{4, 8, '['})
	result.Write(marshalInteger(1))
	result.WriteByte('[')
	result.Write(marshalInteger(3))
	result.WriteByte('i')
	result.Write(marshalInteger(1))
	writeMarshalString(&result, []byte(name))
	writeMarshalString(&result, zlibStore(source))
	return result.Bytes()
}

func writeMarshalString(target *bytes.Buffer, value []byte) {
	target.WriteByte('"')
	target.Write(marshalInteger(int64(len(value))))
	target.Write(value)
}

func marshalInteger(value int64) []byte {
	if value == 0 {
		return []byte{0}
	}
	if value > 0 && value < 123 {
		return []byte{byte(value + 5)}
	}
	if value < 0 && value > -124 {
		return []byte{byte(int8(value - 5))}
	}
	negative := value < 0
	encoded := make([]byte, 0, 8)
	remaining := value
	for len(encoded) < 8 {
		encoded = append(encoded, byte(remaining))
		remaining >>= 8
		if (!negative && remaining == 0) || (negative && remaining == -1) {
			break
		}
	}
	prefix := int8(len(encoded))
	if negative {
		prefix = -prefix
	}
	return append([]byte{byte(prefix)}, encoded...)
}
