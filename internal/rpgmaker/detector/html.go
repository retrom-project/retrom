package detector

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"path"
	"strings"

	"retrom/internal/rpgmaker/nativeweb"
)

var errInvalidWebReference = errors.New("invalid web project reference")

type htmlToken struct {
	name  string
	attrs map[string]string
}

type htmlValidator struct {
	files         *catalog
	baseDirectory string
}

func validateIndexHTML(contents []byte, files *catalog) error {
	validator := &htmlValidator{files: files}
	return tokenizeHTML(string(contents), validator.visit)
}

func (validator *htmlValidator) visit(token htmlToken) error {
	if err := validateNavigationAttributes(token); err != nil {
		return err
	}
	if token.name != "base" {
		return validateHTMLReferences(token, validator.baseDirectory, validator.files)
	}
	value := strings.TrimSpace(token.attrs["href"])
	if value == "" {
		return nil
	}
	resolved, external, err := resolveProjectBase(value)
	if external {
		return newError(CodeNativeDependencyUnsupported, "external base URL is forbidden", nil)
	}
	if err != nil {
		return newError(CodeWebFormatInvalid, "invalid base URL", err)
	}
	validator.baseDirectory = resolved
	return nil
}

func validateNavigationAttributes(token htmlToken) error {
	if value, exists := token.attrs["target"]; exists {
		target := strings.ToLower(strings.TrimSpace(value))
		if target == "_blank" || target == "_top" || target == "_parent" {
			return newError(CodeNativeDependencyUnsupported, "popup or top navigation is forbidden", nil)
		}
	}
	_, action := token.attrs["action"]
	_, formAction := token.attrs["formaction"]
	if (token.name == "form" && action) || formAction {
		return newError(CodeNativeDependencyUnsupported, "form action is forbidden", nil)
	}
	if token.name == "meta" && strings.EqualFold(token.attrs["http-equiv"], "refresh") {
		return newError(CodeNativeDependencyUnsupported, "meta refresh is forbidden", nil)
	}
	return nil
}

func resolveProjectBase(reference string) (string, bool, error) {
	reference = html.UnescapeString(strings.TrimSpace(reference))
	parsed, err := url.Parse(reference)
	if err != nil {
		return "", false, fmt.Errorf("%w: parse base URL: %w", errInvalidWebReference, err)
	}
	if parsed.Scheme != "" || parsed.Host != "" || strings.HasPrefix(reference, "//") {
		return "", true, nil
	}
	if strings.HasPrefix(parsed.Path, "/") {
		return "", false, errInvalidWebReference
	}
	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return "", false, fmt.Errorf("%w: decode base URL: %w", errInvalidWebReference, err)
	}
	cleaned := path.Clean(decodedPath)
	if cleaned == "." {
		return "", false, nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || !validIndexedPath(cleaned) {
		return "", false, errInvalidWebReference
	}
	return cleaned, false, nil
}

func validateHTMLReferences(token htmlToken, baseDirectory string, files *catalog) error {
	references := make(map[string]string)
	if value, exists := token.attrs["src"]; exists {
		references["src"] = value
	}
	if token.name == "link" || token.name == "a" {
		if value, exists := token.attrs["href"]; exists {
			references["href"] = value
		}
	}
	if value, exists := token.attrs["poster"]; exists {
		references["poster"] = value
	}
	for attribute, reference := range references {
		if token.name == "a" && strings.HasPrefix(strings.TrimSpace(reference), "#") {
			continue
		}
		if allowedEmbeddedURL(token.name, reference) {
			continue
		}
		resolved, external, err := resolveProjectReference(baseDirectory, reference)
		if external {
			return newError(CodeNativeDependencyUnsupported, fmt.Sprintf("external %s[%s] URL", token.name, attribute), nil)
		}
		if err == nil && nativeweb.NativeExecutable(resolved) {
			return newError(CodeNativeDependencyUnsupported, fmt.Sprintf("native %s[%s] dependency", token.name, attribute), nil)
		}
		if err != nil || !files.exists(resolved) {
			return newError(CodeWebFormatInvalid, fmt.Sprintf("invalid or missing reference %q", reference), err)
		}
	}
	return nil
}

func allowedEmbeddedURL(tagName, reference string) bool {
	lowered := strings.ToLower(strings.TrimSpace(reference))
	if strings.HasPrefix(lowered, "data:") {
		return tagName == "img"
	}
	if strings.HasPrefix(lowered, "blob:") {
		return tagName == "img" || tagName == "audio" || tagName == "video" || tagName == "source"
	}
	return false
}

func resolveProjectReference(baseDirectory, reference string) (string, bool, error) {
	reference = html.UnescapeString(strings.TrimSpace(reference))
	parsed, err := url.Parse(reference)
	if err != nil {
		return "", false, fmt.Errorf("%w: parse resource URL: %w", errInvalidWebReference, err)
	}
	if parsed.Scheme != "" || parsed.Host != "" || strings.HasPrefix(reference, "//") {
		return "", true, nil
	}
	if parsed.Path == "" || strings.HasPrefix(parsed.Path, "/") {
		return "", false, errInvalidWebReference
	}
	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return "", false, fmt.Errorf("%w: decode resource URL: %w", errInvalidWebReference, err)
	}
	joined := path.Clean(path.Join(baseDirectory, decodedPath))
	if joined == "." || joined == ".." || strings.HasPrefix(joined, "../") || !validIndexedPath(joined) {
		return "", false, errInvalidWebReference
	}
	return joined, false, nil
}

func tokenizeHTML(document string, visit func(htmlToken) error) error {
	position := 0
	for position < len(document) {
		token, closing, nextPosition, done, err := nextHTMLToken(document, position)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		position = nextPosition
		if closing || token.name == "" {
			continue
		}
		if err := visit(token); err != nil {
			return err
		}
		position, err = advanceRawText(document, position, token)
		if err != nil {
			return err
		}
	}
	return nil
}

func nextHTMLToken(document string, position int) (htmlToken, bool, int, bool, error) {
	for position < len(document) {
		open := strings.IndexByte(document[position:], '<')
		if open < 0 {
			return htmlToken{}, false, 0, true, nil
		}
		position += open
		if strings.HasPrefix(document[position:], "<!--") {
			closePosition := strings.Index(document[position+4:], "-->")
			if closePosition < 0 {
				return htmlToken{}, false, 0, false,
					newError(CodeWebFormatInvalid, "unterminated HTML comment", nil)
			}
			position += closePosition + 7
			continue
		}
		end, err := findTagEnd(document, position+1)
		if err != nil {
			return htmlToken{}, false, 0, false, err
		}
		token, closing, err := parseHTMLTag(document[position+1 : end])
		return token, closing, end + 1, false, err
	}
	return htmlToken{}, false, 0, true, nil
}

func advanceRawText(document string, position int, token htmlToken) (int, error) {
	if token.name != "script" && token.name != "style" {
		return position, nil
	}
	closingTag := "</" + token.name
	closePosition := strings.Index(strings.ToLower(document[position:]), closingTag)
	if closePosition < 0 {
		return 0, newError(CodeWebFormatInvalid, "unterminated raw-text element", nil)
	}
	if token.name == "script" {
		_, externalScript := token.attrs["src"]
		inlineScript := []byte(document[position : position+closePosition])
		if !externalScript && strings.TrimSpace(string(inlineScript)) != "" {
			return 0, newError(CodeNativeBridgeUnsupported, "inline executable script cannot be isolated", nil)
		}
	}
	return position + closePosition, nil
}

func findTagEnd(document string, start int) (int, error) {
	quote := byte(0)
	for position := start; position < len(document); position++ {
		current := document[position]
		if quote != 0 {
			if current == quote {
				quote = 0
			}
			continue
		}
		if current == '\'' || current == '"' {
			quote = current
			continue
		}
		if current == '>' {
			return position, nil
		}
	}
	return 0, newError(CodeWebFormatInvalid, "unterminated HTML tag", nil)
}

func parseHTMLTag(raw string) (htmlToken, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "!") || strings.HasPrefix(raw, "?") {
		return htmlToken{}, false, nil
	}
	closing := strings.HasPrefix(raw, "/")
	if closing {
		return htmlToken{}, true, nil
	}
	position := 0
	name := readHTMLName(raw, &position)
	if name == "" {
		return htmlToken{}, false, newError(CodeWebFormatInvalid, "HTML tag name is empty", nil)
	}
	token := htmlToken{name: strings.ToLower(name), attrs: make(map[string]string)}
	for position < len(raw) {
		skipHTMLSpace(raw, &position)
		if position >= len(raw) || raw[position] == '/' {
			break
		}
		attribute := strings.ToLower(readHTMLName(raw, &position))
		if attribute == "" {
			return htmlToken{}, false, newError(CodeWebFormatInvalid, "invalid HTML attribute", nil)
		}
		skipHTMLSpace(raw, &position)
		value := ""
		if position < len(raw) && raw[position] == '=' {
			position++
			skipHTMLSpace(raw, &position)
			var err error
			value, err = readHTMLAttributeValue(raw, &position)
			if err != nil {
				return htmlToken{}, false, err
			}
		}
		if _, duplicate := token.attrs[attribute]; duplicate {
			return htmlToken{}, false, newError(CodeWebFormatInvalid, "duplicate HTML attribute", nil)
		}
		token.attrs[attribute] = html.UnescapeString(value)
	}
	return token, false, nil
}

func readHTMLName(raw string, position *int) string {
	start := *position
	for *position < len(raw) {
		current := raw[*position]
		if isHTMLSpace(current) || current == '=' || current == '/' {
			break
		}
		*position++
	}
	return raw[start:*position]
}

func readHTMLAttributeValue(raw string, position *int) (string, error) {
	if *position >= len(raw) {
		return "", newError(CodeWebFormatInvalid, "missing HTML attribute value", nil)
	}
	quote := raw[*position]
	if quote == '\'' || quote == '"' {
		*position++
		start := *position
		for *position < len(raw) && raw[*position] != quote {
			*position++
		}
		if *position >= len(raw) {
			return "", newError(CodeWebFormatInvalid, "unterminated HTML attribute", nil)
		}
		value := raw[start:*position]
		*position++
		return value, nil
	}
	start := *position
	for *position < len(raw) && !isHTMLSpace(raw[*position]) && raw[*position] != '/' {
		*position++
	}
	return raw[start:*position], nil
}

func skipHTMLSpace(raw string, position *int) {
	for *position < len(raw) && isHTMLSpace(raw[*position]) {
		*position++
	}
}

func isHTMLSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n' || value == '\f'
}
