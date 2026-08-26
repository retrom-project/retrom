package checkpoint

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The schema has fixed ASCII property names and integral numbers. Writing them
// in UTF-16 lexical property order plus ECMAScript-compatible strings is the
// complete RFC 8785 subset required by NATIVE_SAVE_BUNDLE_V1.
func marshalCanonical(manifest Manifest) []byte {
	var output bytes.Buffer
	output.WriteString(`{"engine":`)
	writeJSONString(&output, string(manifest.Engine))
	output.WriteString(`,"entries":[`)
	for index, entry := range manifest.Entries {
		if index > 0 {
			output.WriteByte(',')
		}
		output.WriteString(`{"key":`)
		writeJSONString(&output, entry.Key)
		output.WriteString(`,"mediaType":`)
		writeJSONString(&output, entry.MediaType)
		fmt.Fprintf(&output, `,"offset":%d`, entry.Offset)
		output.WriteString(`,"sha256":`)
		writeJSONString(&output, entry.SHA256)
		fmt.Fprintf(&output, `,"sizeBytes":%d`, entry.SizeBytes)
		output.WriteString(`,"store":`)
		writeJSONString(&output, string(entry.Store))
		output.WriteByte('}')
	}
	fmt.Fprintf(&output, `],"resumeSlot":%d,"schemaVersion":%d}`, manifest.ResumeSlot, manifest.SchemaVersion)
	return output.Bytes()
}

func writeJSONString(output *bytes.Buffer, value string) {
	output.WriteByte('"')
	for _, current := range value {
		switch current {
		case '"', '\\':
			output.WriteByte('\\')
			output.WriteRune(current)
		case '\b':
			output.WriteString(`\b`)
		case '\t':
			output.WriteString(`\t`)
		case '\n':
			output.WriteString(`\n`)
		case '\f':
			output.WriteString(`\f`)
		case '\r':
			output.WriteString(`\r`)
		default:
			if current < 0x20 {
				fmt.Fprintf(output, `\u%04x`, current)
			} else {
				output.WriteRune(current)
			}
		}
	}
	output.WriteByte('"')
}

func unmarshalCanonical(contents []byte) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, ErrInvalid
	}
	if decoder.More() {
		return Manifest{}, ErrInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Manifest{}, ErrInvalid
	}
	canonical := marshalCanonical(manifest)
	if !bytes.Equal(canonical, contents) {
		return Manifest{}, ErrInvalid
	}
	return manifest, nil
}
