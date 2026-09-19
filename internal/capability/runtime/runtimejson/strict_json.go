// Package runtimejson implements the Provider-safe JSON grammar and bounded
// schema rules without runtime state or I/O.
package runtimejson

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

var errStrictJSON = errors.New("invalid strict JSON")

type strictJSONParser struct {
	contents []byte
	offset   int
}

func ParseStrictJSON(contents []byte) (any, error) {
	if !utf8.Valid(contents) {
		return nil, errStrictJSON
	}
	parser := &strictJSONParser{contents: contents}
	value, err := parser.value()
	parser.space()
	if err != nil || parser.offset != len(contents) {
		return nil, errStrictJSON
	}
	return value, nil
}

func (parser *strictJSONParser) array() (any, error) {
	parser.offset++
	parser.space()
	result := []any{}
	if parser.take(']') {
		return result, nil
	}
	for {
		value, err := parser.value()
		if err != nil {
			return nil, err
		}
		result = append(result, value)
		parser.space()
		if parser.take(']') {
			return result, nil
		}
		if !parser.take(',') {
			return nil, errStrictJSON
		}
		parser.space()
	}
}

func (parser *strictJSONParser) hexUnit() (int64, error) {
	if parser.offset+4 > len(parser.contents) {
		return 0, errStrictJSON
	}
	value, err := strconv.ParseInt(string(parser.contents[parser.offset:parser.offset+4]), 16, 32)
	parser.offset += 4
	if err != nil {
		return 0, fmt.Errorf("parse JSON Unicode escape: %w", err)
	}
	return value, nil
}

func (parser *strictJSONParser) integer() (any, error) {
	start := parser.offset
	if parser.take('-') && parser.offset >= len(parser.contents) {
		return nil, errStrictJSON
	}
	if parser.take('0') {
		if parser.offset < len(parser.contents) && decimalDigit(parser.contents[parser.offset]) {
			return nil, errStrictJSON
		}
	} else {
		if parser.offset >= len(parser.contents) || !nonzeroDecimalDigit(parser.contents[parser.offset]) {
			return nil, errStrictJSON
		}
		for parser.offset < len(parser.contents) && decimalDigit(parser.contents[parser.offset]) {
			parser.offset++
		}
	}
	value, err := strconv.ParseInt(string(parser.contents[start:parser.offset]), 10, 64)
	if err != nil || value < -9007199254740991 || value > 9007199254740991 {
		return nil, errStrictJSON
	}
	return value, nil
}

func (parser *strictJSONParser) literal(text string, value any) (any, error) {
	if parser.offset+len(text) > len(parser.contents) ||
		string(parser.contents[parser.offset:parser.offset+len(text)]) != text {
		return nil, errStrictJSON
	}
	parser.offset += len(text)
	return value, nil
}

func (parser *strictJSONParser) object() (any, error) {
	parser.offset++
	parser.space()
	result := map[string]any{}
	if parser.take('}') {
		return result, nil
	}
	for {
		key, err := parser.string()
		if err != nil {
			return nil, err
		}
		if _, duplicate := result[key]; duplicate {
			return nil, errStrictJSON
		}
		parser.space()
		if !parser.take(':') {
			return nil, errStrictJSON
		}
		value, err := parser.value()
		if err != nil {
			return nil, err
		}
		result[key] = value
		parser.space()
		if parser.take('}') {
			return result, nil
		}
		if !parser.take(',') {
			return nil, errStrictJSON
		}
		parser.space()
	}
}

func (parser *strictJSONParser) space() {
	for parser.offset < len(parser.contents) &&
		bytes.ContainsRune([]byte(" \t\r\n"), rune(parser.contents[parser.offset])) {
		parser.offset++
	}
}

func (parser *strictJSONParser) string() (string, error) {
	if !parser.take('"') {
		return "", errStrictJSON
	}
	var result strings.Builder
	for parser.offset < len(parser.contents) {
		character := parser.contents[parser.offset]
		if character == '"' {
			parser.offset++
			return result.String(), nil
		}
		if character == '\\' {
			parser.offset++
			if err := parser.writeEscape(&result); err != nil {
				return "", err
			}
			continue
		}
		if character < 0x20 {
			return "", errStrictJSON
		}
		r, size := utf8.DecodeRune(parser.contents[parser.offset:])
		if r == utf8.RuneError && size == 1 || r >= 0xd800 && r <= 0xdfff {
			return "", errStrictJSON
		}
		result.WriteRune(r)
		parser.offset += size
	}
	return "", errStrictJSON
}

func (parser *strictJSONParser) take(expected byte) bool {
	if parser.offset < len(parser.contents) && parser.contents[parser.offset] == expected {
		parser.offset++
		return true
	}
	return false
}

func (parser *strictJSONParser) value() (any, error) {
	parser.space()
	if parser.offset >= len(parser.contents) {
		return nil, errStrictJSON
	}
	switch parser.contents[parser.offset] {
	case '"':
		return parser.string()
	case '{':
		return parser.object()
	case '[':
		return parser.array()
	case 't':
		return parser.literal("true", true)
	case 'f':
		return parser.literal("false", false)
	case 'n':
		return parser.literal("null", nil)
	default:
		return parser.integer()
	}
}

func (parser *strictJSONParser) writeEscape(result *strings.Builder) error {
	if parser.offset >= len(parser.contents) {
		return errStrictJSON
	}
	escape := parser.contents[parser.offset]
	parser.offset++
	switch escape {
	case '"', '\\', '/':
		result.WriteByte(escape)
	case 'b':
		result.WriteByte('\b')
	case 'f':
		result.WriteByte('\f')
	case 'n':
		result.WriteByte('\n')
	case 'r':
		result.WriteByte('\r')
	case 't':
		result.WriteByte('\t')
	case 'u':
		return parser.writeUnicodeEscape(result)
	default:
		return errStrictJSON
	}
	return nil
}

func (parser *strictJSONParser) writeUnicodeEscape(result *strings.Builder) error {
	first, err := parser.hexUnit()
	if err != nil {
		return err
	}
	switch {
	case first >= 0xd800 && first <= 0xdbff:
		if parser.offset+2 > len(parser.contents) || string(parser.contents[parser.offset:parser.offset+2]) != `\u` {
			return errStrictJSON
		}
		parser.offset += 2
		second, secondErr := parser.hexUnit()
		if secondErr != nil || second < 0xdc00 || second > 0xdfff {
			return errStrictJSON
		}
		result.WriteRune(utf16.DecodeRune(rune(first), rune(second)))
	case first >= 0xdc00 && first <= 0xdfff:
		return errStrictJSON
	default:
		result.WriteRune(rune(first))
	}
	return nil
}

func decimalDigit(value byte) bool { return value >= '0' && value <= '9' }

func nonzeroDecimalDigit(value byte) bool { return value >= '1' && value <= '9' }
