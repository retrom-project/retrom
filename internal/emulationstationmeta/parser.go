package emulationstationmeta

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

var utf8BOM = []byte{0xef, 0xbb, 0xbf}

// Parse validates one bounded gamelist.xml and returns its canonical Retrom
// projection. releaseYearMax is frozen by the calling scan plan.
func Parse(contents []byte, releaseYearMax int) (Document, error) {
	return ParseContext(context.Background(), contents, releaseYearMax)
}

// ParseContext is Parse with bounded cancellation checks while tokens are
// consumed. This keeps large-but-valid XML from delaying scan cancellation.
func ParseContext(ctx context.Context, contents []byte, releaseYearMax int) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, fmt.Errorf("emulationstationmeta/parse cancelled: %w", err)
	}
	if len(contents) > MaxGameListBytes {
		return Document{}, ErrTooLarge
	}
	contents = bytes.TrimPrefix(contents, utf8BOM)
	if !utf8.Valid(contents) || bytes.Contains(contents, utf8BOM) {
		return Document{}, ErrInvalidUTF8
	}

	reader := newTokenReader(ctx, contents)
	ignored := newIgnoredCollector()
	document, err := parseDocument(reader, releaseYearMax, ignored)
	if err != nil {
		return Document{}, err
	}
	document.IgnoredFields, document.IgnoredFieldOtherCount = ignored.result()
	return document, nil
}

type tokenReader struct {
	ctx        context.Context
	decoder    *xml.Decoder
	tokenCount int
	depth      int
}

func newTokenReader(ctx context.Context, contents []byte) *tokenReader {
	decoder := xml.NewDecoder(bytes.NewReader(contents))
	decoder.Strict = true
	decoder.CharsetReader = func(string, io.Reader) (io.Reader, error) {
		return nil, ErrInvalidUTF8
	}
	return &tokenReader{ctx: ctx, decoder: decoder}
}

func (reader *tokenReader) next() (xml.Token, error) {
	before := reader.decoder.InputOffset()
	token, err := reader.decoder.Token()
	if err != nil {
		return nil, classifyDecoderError(err)
	}
	if err := reader.recordToken(token, before); err != nil {
		return nil, err
	}
	return token, nil
}

func classifyDecoderError(err error) error {
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	if errors.Is(err, ErrInvalidUTF8) {
		return ErrInvalidUTF8
	}
	return ErrInvalidXML
}

func (reader *tokenReader) recordToken(token xml.Token, before int64) error {
	reader.tokenCount++
	if reader.tokenCount%256 == 0 {
		if err := reader.ctx.Err(); err != nil {
			return fmt.Errorf("emulationstationmeta/parse cancelled: %w", err)
		}
	}
	if reader.tokenCount > MaxXMLTokens || reader.decoder.InputOffset()-before > MaxXMLTokenBytes {
		return ErrLimitExceeded
	}
	switch value := token.(type) {
	case xml.StartElement:
		return reader.recordStart(value)
	case xml.EndElement:
		return reader.recordEnd()
	case xml.CharData:
		if bytes.Contains(value, utf8BOM) {
			return ErrInvalidUTF8
		}
	case xml.Directive:
		return ErrInvalidXML
	case xml.ProcInst:
		return reader.validateProcessingInstruction(value)
	}
	return nil
}

func (reader *tokenReader) recordStart(element xml.StartElement) error {
	reader.depth++
	if reader.depth > MaxXMLDepth || len(element.Attr) > MaxXMLAttributes {
		return ErrLimitExceeded
	}
	for _, attribute := range element.Attr {
		if strings.ContainsRune(attribute.Value, '\ufeff') {
			return ErrInvalidUTF8
		}
	}
	return nil
}

func (reader *tokenReader) recordEnd() error {
	reader.depth--
	if reader.depth < 0 {
		return ErrInvalidXML
	}
	return nil
}

func (reader *tokenReader) validateProcessingInstruction(instruction xml.ProcInst) error {
	if instruction.Target != "xml" || reader.tokenCount != 1 || reader.depth != 0 {
		return ErrInvalidXML
	}
	return nil
}

func parseDocument(reader *tokenReader, releaseYearMax int, ignored *ignoredCollector) (Document, error) {
	for {
		token, err := reader.next()
		if errors.Is(err, io.EOF) {
			return Document{}, ErrInvalidRoot
		}
		if err != nil {
			return Document{}, err
		}
		switch value := token.(type) {
		case xml.ProcInst, xml.Comment:
			continue
		case xml.CharData:
			if !xmlWhitespace(value) {
				return Document{}, ErrInvalidRoot
			}
		case xml.StartElement:
			if value.Name.Local != "gameList" || value.Name.Space != "" || hasNamespaceAttribute(value) {
				return Document{}, ErrInvalidRoot
			}
			document, parseErr := parseRoot(reader, value, releaseYearMax, ignored)
			if parseErr != nil {
				return Document{}, parseErr
			}
			if parseErr = consumeDocumentTail(reader); parseErr != nil {
				return Document{}, parseErr
			}
			return document, nil
		default:
			return Document{}, ErrInvalidRoot
		}
	}
}

func consumeDocumentTail(reader *tokenReader) error {
	for {
		token, err := reader.next()
		if errors.Is(err, io.EOF) {
			if reader.depth != 0 {
				return ErrInvalidXML
			}
			return nil
		}
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.Comment:
			continue
		case xml.CharData:
			if xmlWhitespace(value) {
				continue
			}
		}
		return ErrInvalidRoot
	}
}

func parseRoot(
	reader *tokenReader,
	root xml.StartElement,
	releaseYearMax int,
	ignored *ignoredCollector,
) (Document, error) {
	document := Document{Games: make([]Game, 0)}
	for {
		token, err := reader.next()
		if err != nil {
			return Document{}, err
		}
		switch value := token.(type) {
		case xml.Comment:
			continue
		case xml.CharData:
			if !xmlWhitespace(value) {
				return Document{}, ErrInvalidXML
			}
		case xml.StartElement:
			if err := consumeRootChild(reader, value, &document, releaseYearMax, ignored); err != nil {
				return Document{}, err
			}
		case xml.EndElement:
			if value.Name != root.Name {
				return Document{}, ErrInvalidXML
			}
			return document, nil
		default:
			return Document{}, ErrInvalidXML
		}
	}
}

func consumeRootChild(
	reader *tokenReader,
	element xml.StartElement,
	document *Document,
	releaseYearMax int,
	ignored *ignoredCollector,
) error {
	if err := validateUnnamespaced(element); err != nil {
		return err
	}
	switch element.Name.Local {
	case "game":
		if len(document.Games) >= MaxGames {
			return ErrLimitExceeded
		}
		game, err := parseGame(reader, element, len(document.Games)+1, releaseYearMax, ignored)
		if err != nil {
			return err
		}
		document.Games = append(document.Games, game)
		return nil
	case "folder":
		document.FolderEntryCount++
	case "provider":
		document.ProviderPresent = true
	default:
		ignored.add(element.Name.Local)
	}
	return skipElement(reader)
}

func parseGame(
	reader *tokenReader,
	start xml.StartElement,
	ordinal, releaseYearMax int,
	ignored *ignoredCollector,
) (Game, error) {
	builder := newGameBuilder(ordinal, releaseYearMax, start.Attr, ignored)
	fieldCount := 0
	for {
		token, err := reader.next()
		if err != nil {
			return Game{}, err
		}
		switch value := token.(type) {
		case xml.Comment:
			continue
		case xml.CharData:
			if !xmlWhitespace(value) {
				return Game{}, ErrInvalidXML
			}
		case xml.StartElement:
			if err := validateUnnamespaced(value); err != nil {
				return Game{}, err
			}
			fieldCount++
			if fieldCount > MaxGameFields {
				return Game{}, ErrLimitExceeded
			}
			field, readErr := readField(reader, value)
			if readErr != nil {
				return Game{}, readErr
			}
			builder.consume(field)
		case xml.EndElement:
			if value.Name != start.Name {
				return Game{}, ErrInvalidXML
			}
			return builder.finish(), nil
		default:
			return Game{}, ErrInvalidXML
		}
	}
}

type rawField struct {
	name       string
	text       string
	structured bool
	attributes []xml.Attr
}

func readField(reader *tokenReader, start xml.StartElement) (rawField, error) {
	field := rawField{name: start.Name.Local, attributes: start.Attr}
	var text strings.Builder
	nestedDepth := 0
	for {
		token, err := reader.next()
		if err != nil {
			return rawField{}, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if err := validateUnnamespaced(value); err != nil {
				return rawField{}, err
			}
			field.structured = true
			nestedDepth++
		case xml.EndElement:
			if nestedDepth == 0 {
				if value.Name != start.Name {
					return rawField{}, ErrInvalidXML
				}
				field.text = text.String()
				return field, nil
			}
			nestedDepth--
		case xml.CharData:
			if nestedDepth == 0 {
				_, _ = text.Write(value)
			}
		case xml.Comment:
			continue
		default:
			return rawField{}, ErrInvalidXML
		}
	}
}

func skipElement(reader *tokenReader) error {
	nestedDepth := 0
	for {
		token, err := reader.next()
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if err := validateUnnamespaced(value); err != nil {
				return err
			}
			nestedDepth++
		case xml.EndElement:
			if nestedDepth == 0 {
				return nil
			}
			nestedDepth--
		}
	}
}

func validateUnnamespaced(element xml.StartElement) error {
	if element.Name.Space != "" || hasNamespaceAttribute(element) {
		return ErrInvalidXML
	}
	return nil
}

func hasNamespaceAttribute(element xml.StartElement) bool {
	for _, attribute := range element.Attr {
		if attribute.Name.Space != "" || attribute.Name.Local == "xmlns" {
			return true
		}
	}
	return false
}

func xmlWhitespace(value []byte) bool {
	for _, character := range value {
		if character != ' ' && character != '\t' && character != '\n' && character != '\r' {
			return false
		}
	}
	return true
}

type ignoredCollector struct {
	names map[string]struct{}
}

func newIgnoredCollector() *ignoredCollector {
	return &ignoredCollector{names: make(map[string]struct{})}
}

func (collector *ignoredCollector) add(name string) {
	collector.names[reportedName(name)] = struct{}{}
}

func (collector *ignoredCollector) result() ([]string, int) {
	names := make([]string, 0, len(collector.names))
	for name := range collector.names {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) <= MaxIgnoredFieldNames {
		return names, 0
	}
	return names[:MaxIgnoredFieldNames], len(names) - MaxIgnoredFieldNames
}

func reportedName(name string) string {
	const maximumBytes = 255
	if len(name) <= maximumBytes {
		return name
	}
	for maximum := maximumBytes; maximum > 0; maximum-- {
		if utf8.RuneStart(name[maximum]) {
			return name[:maximum]
		}
	}
	return ""
}
