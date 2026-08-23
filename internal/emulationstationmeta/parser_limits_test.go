package emulationstationmeta

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParseAcceptsOnlyStrictUTF8XMLSurface(t *testing.T) {
	t.Parallel()
	accepted := []string{
		`<gameList/>`,
		`<?xml version="1.0"?><gameList/>`,
		`<?xml version="1.0" encoding="utf-8"?><gameList/>`,
		"\xef\xbb\xbf<?xml version=\"1.0\" encoding=\"UTF-8\"?><gameList/>",
		"<!--before-->\n<gameList ordinary=\"attribute\"><!--inside--></gameList>\n<!--after-->",
		`<gameList><game><path>safe.gba</path><desc><![CDATA[one < two]]></desc></game></gameList>`,
	}
	for index, document := range accepted {
		t.Run(fmt.Sprintf("accepted_%d", index), func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(document), 2027); err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
		})
	}
}

func TestParseRejectsEncodingAndXMLSecurityViolations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		document []byte
		want     error
	}{
		{name: "invalid utf8", document: []byte("<gameList>\xff</gameList>"), want: ErrInvalidUTF8},
		{name: "second bom", document: []byte("\xef\xbb\xbf\xef\xbb\xbf<gameList/>"), want: ErrInvalidUTF8},
		{name: "late bom", document: []byte("<gameList>\xef\xbb\xbf</gameList>"), want: ErrInvalidUTF8},
		{name: "numeric bom text", document: []byte(`<gameList>&#xfeff;</gameList>`), want: ErrInvalidUTF8},
		{name: "numeric bom attribute", document: []byte(`<gameList source="&#xfeff;"/>`), want: ErrInvalidUTF8},
		{name: "non utf8 declaration", document: []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><gameList/>`), want: ErrInvalidUTF8},
		{name: "unsupported utf8 spelling", document: []byte(`<?xml version="1.0" encoding="UTF8"?><gameList/>`), want: ErrInvalidUTF8},
		{name: "doctype", document: []byte(`<!DOCTYPE gameList><gameList/>`), want: ErrInvalidXML},
		{name: "external entity", document: []byte(`<!DOCTYPE gameList [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><gameList>&xxe;</gameList>`), want: ErrInvalidXML},
		{name: "undeclared entity", document: []byte(`<gameList>&private;</gameList>`), want: ErrInvalidXML},
		{name: "processing instruction before", document: []byte(`<?unsafe value?><gameList/>`), want: ErrInvalidXML},
		{name: "processing instruction inside", document: []byte(`<gameList><?unsafe value?></gameList>`), want: ErrInvalidXML},
		{name: "processing instruction after", document: []byte(`<gameList/><?unsafe value?>`), want: ErrInvalidXML},
		{name: "xml declaration after comment", document: []byte(`<!--x--><?xml version="1.0"?><gameList/>`), want: ErrInvalidXML},
		{name: "default namespace", document: []byte(`<gameList xmlns="urn:test"/>`), want: ErrInvalidRoot},
		{name: "empty namespace declaration", document: []byte(`<gameList xmlns=""/>`), want: ErrInvalidRoot},
		{name: "prefixed root", document: []byte(`<x:gameList xmlns:x="urn:test"/>`), want: ErrInvalidRoot},
		{name: "nested namespace", document: []byte(`<gameList><game xmlns:x="urn:test"><path>safe.gba</path></game></gameList>`), want: ErrInvalidXML},
		{name: "namespaced attribute", document: []byte(`<gameList><game xml:lang="en"><path>safe.gba</path></game></gameList>`), want: ErrInvalidXML},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(test.document, 2027)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseRequiresExactRootAndRootOnlyContentOutside(t *testing.T) {
	t.Parallel()
	tests := []string{
		"", " \n\t", `<gamelist/>`, `<GameList/>`, `<gameList></gameList><gameList/>`,
		`text<gameList/>`, `<gameList/>text`, `<gameList>text</gameList>`, `<gameList><game/>tail</gameList>`,
	}
	for index, document := range tests {
		t.Run(fmt.Sprintf("invalid_root_%d", index), func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(document), 2027)
			if !errors.Is(err, ErrInvalidRoot) && !errors.Is(err, ErrInvalidXML) {
				t.Fatalf("error = %v, want root/XML error", err)
			}
		})
	}
}

func TestParseEnforcesDepthLimit(t *testing.T) {
	t.Parallel()
	accepted := nestedUnknownXML(MaxXMLDepth - 1)
	if _, err := Parse([]byte(accepted), 2027); err != nil {
		t.Fatalf("depth %d rejected: %v", MaxXMLDepth, err)
	}
	rejected := nestedUnknownXML(MaxXMLDepth)
	if _, err := Parse([]byte(rejected), 2027); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("depth %d error = %v", MaxXMLDepth+1, err)
	}
}

func TestParseEnforcesAttributeLimit(t *testing.T) {
	t.Parallel()
	accepted := `<gameList><unknown` + xmlAttributes(MaxXMLAttributes) + `/></gameList>`
	if _, err := Parse([]byte(accepted), 2027); err != nil {
		t.Fatalf("%d attributes rejected: %v", MaxXMLAttributes, err)
	}
	rejected := `<gameList><unknown` + xmlAttributes(MaxXMLAttributes+1) + `/></gameList>`
	if _, err := Parse([]byte(rejected), 2027); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("%d attributes error = %v", MaxXMLAttributes+1, err)
	}
}

func TestParseEnforcesGameFieldLimit(t *testing.T) {
	t.Parallel()
	accepted := `<gameList><game>` + strings.Repeat(`<unknown/>`, MaxGameFields) + `</game></gameList>`
	if _, err := Parse([]byte(accepted), 2027); err != nil {
		t.Fatalf("%d fields rejected: %v", MaxGameFields, err)
	}
	rejected := `<gameList><game>` + strings.Repeat(`<unknown/>`, MaxGameFields+1) + `</game></gameList>`
	if _, err := Parse([]byte(rejected), 2027); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("%d fields error = %v", MaxGameFields+1, err)
	}
}

func TestParseEnforcesGameLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("constructs the exact 100,001-game boundary")
	}
	document := `<gameList>` + strings.Repeat(`<game/>`, MaxGames+1) + `</gameList>`
	if _, err := Parse([]byte(document), 2027); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("%d games error = %v", MaxGames+1, err)
	}
}

func TestParseEnforcesTokenSizeAndCountLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("constructs exact million-token boundaries")
	}
	accepted := `<gameList><unknown>` + strings.Repeat("x", MaxXMLTokenBytes) + `</unknown></gameList>`
	if _, err := Parse([]byte(accepted), 2027); err != nil {
		t.Fatalf("maximum token rejected: %v", err)
	}
	overlarge := `<gameList><unknown>` + strings.Repeat("x", MaxXMLTokenBytes+1) + `</unknown></gameList>`
	if _, err := Parse([]byte(overlarge), 2027); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("overlarge token error = %v", err)
	}
	manyTokens := `<gameList>` + strings.Repeat(`<x/>`, MaxXMLTokens/2) + `</gameList>`
	if _, err := Parse([]byte(manyTokens), 2027); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("too many tokens error = %v", err)
	}
}

func TestParseEnforcesFileByteLimitBeforeXMLParsing(t *testing.T) {
	t.Parallel()
	contents := make([]byte, MaxGameListBytes+1)
	if _, err := Parse(contents, 2027); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("overlarge file error = %v", err)
	}
}

func nestedUnknownXML(nesting int) string {
	return `<gameList>` + strings.Repeat(`<unknown>`, nesting) +
		strings.Repeat(`</unknown>`, nesting) + `</gameList>`
}

func xmlAttributes(count int) string {
	var attributes strings.Builder
	for index := 0; index < count; index++ {
		_, _ = fmt.Fprintf(&attributes, ` a%d=""`, index)
	}
	return attributes.String()
}
