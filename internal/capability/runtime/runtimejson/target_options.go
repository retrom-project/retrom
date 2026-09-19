package runtimejson

import (
	"encoding/json"
	"regexp"
	"sort"
	"unicode/utf8"
)

// DecodeTargetOptionsSchema parses and normalizes a closed Provider schema.
// Failure returns before the caller replaces its existing schema value.
func DecodeTargetOptionsSchema(contents []byte) (map[string]json.RawMessage, error) {
	value, err := ParseStrictJSON(contents)
	object, ok := value.(map[string]any)
	if err != nil || !ok || !validTargetOptionsSchema(object, 0, true) {
		return nil, errStrictJSON
	}
	result := make(map[string]json.RawMessage, len(object))
	for key, child := range object {
		encoded, err := json.Marshal(child)
		if err != nil {
			return nil, errStrictJSON
		}
		result[key] = encoded
	}
	return result, nil
}

// ValidateTargetOptionsSchema checks the exact Provider-owned schema dialect.
func ValidateTargetOptionsSchema(schema map[string]json.RawMessage) bool {
	contents, err := json.Marshal(schema)
	if err != nil {
		return false
	}
	_, err = DecodeTargetOptionsSchema(contents)
	return err == nil
}

// ValidateTargetOptions applies the Provider schema to a closed JSON object.
// The working tree is local to this pure parser; callers retain only JSON bytes.
func ValidateTargetOptions(schema map[string]json.RawMessage, contents json.RawMessage) bool {
	if schema == nil || contents == nil {
		return false
	}
	schemaContents, err := json.Marshal(schema)
	if err != nil {
		return false
	}
	parsedSchema, err := ParseStrictJSON(schemaContents)
	object, schemaOK := parsedSchema.(map[string]any)
	if err != nil || !schemaOK {
		return false
	}
	value, err := ParseStrictJSON(contents)
	candidate, valueOK := value.(map[string]any)
	if err != nil || !valueOK || !ValidJSONValue(candidate, 0, true) {
		return false
	}
	normalized, err := json.Marshal(candidate)
	return err == nil && len(normalized) <= MaxTargetOptionsBytes &&
		validTargetOptionsValue(candidate, object, 0)
}

// TargetOptionPropertyKeys exposes only the object keys required by Host access
// policy. Full Provider validation remains at the existing parser boundary.
func TargetOptionPropertyKeys(contents json.RawMessage) ([]string, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(contents, &object); err != nil || object == nil {
		return nil, false
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, true
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func mapKeysAllowed(value map[string]any, keys ...string) bool {
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = true
	}
	for key := range value {
		if !allowed[key] {
			return false
		}
	}
	return value["type"] != nil
}

const MaxTargetOptionsBytes = 16 * 1024

func nonNegativeSchemaInteger(value any) (int64, bool) {
	result, ok := schemaInteger(value)
	return result, ok && result >= 0
}

func optionalNonNegativeSchemaInteger(value map[string]any, key string, fallback int64) (int64, bool) {
	candidate, exists := value[key]
	if !exists {
		return fallback, true
	}
	return nonNegativeSchemaInteger(candidate)
}

func optionalSchemaInteger(value map[string]any, key string, fallback int64) (int64, bool) {
	candidate, exists := value[key]
	if !exists {
		return fallback, true
	}
	return schemaInteger(candidate)
}

func schemaInteger(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func schemaStringSet(value any, empty bool) ([]string, bool) {
	items, ok := value.([]any)
	if !ok || !empty && len(items) == 0 {
		return nil, false
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, textOK := item.(string)
		if !textOK {
			return nil, false
		}
		result = append(result, text)
	}
	return result, sort.StringsAreSorted(result) && uniqueStrings(result)
}

func targetOptionsBaseType(value string) bool {
	return value == "array" || value == "boolean" || value == "integer" || value == "object" || value == "string"
}

var targetOptionsPropertyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,63}$`)

func targetOptionsSchemaType(value any) (string, bool, bool) {
	if text, ok := value.(string); ok && targetOptionsBaseType(text) {
		return text, false, true
	}
	values, ok := value.([]any)
	if !ok || len(values) != 2 || values[1] != "null" {
		return "", false, false
	}
	text, ok := values[0].(string)
	return text, true, ok && targetOptionsBaseType(text)
}

func uniqueStrings(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] == values[index] {
			return false
		}
	}
	return true
}

func validArrayTargetOptionsSchema(schema map[string]any, depth int) bool {
	if !mapKeysAllowed(schema, "items", "maxItems", "minItems", "type") {
		return false
	}
	item, itemOK := schema["items"].(map[string]any)
	maximum, maximumOK := nonNegativeSchemaInteger(schema["maxItems"])
	minimum, minimumOK := optionalNonNegativeSchemaInteger(schema, "minItems", 0)
	return itemOK && maximumOK && minimumOK && maximum <= 256 && minimum <= maximum &&
		validTargetOptionsSchema(item, depth+1, false)
}

func validArrayTargetOptionsValue(value any, schema map[string]any, depth int) bool {
	items, arrayOK := value.([]any)
	itemSchema, itemOK := schema["items"].(map[string]any)
	minimum, minimumOK := optionalNonNegativeSchemaInteger(schema, "minItems", 0)
	maximum, maximumOK := optionalNonNegativeSchemaInteger(schema, "maxItems", 0)
	if !arrayOK || !itemOK || !minimumOK || !maximumOK ||
		int64(len(items)) < minimum || int64(len(items)) > maximum {
		return false
	}
	for _, item := range items {
		if !validTargetOptionsValue(item, itemSchema, depth+1) {
			return false
		}
	}
	return true
}

func validIntegerTargetOptionsSchema(schema map[string]any) bool {
	if !mapKeysAllowed(schema, "maximum", "minimum", "type") {
		return false
	}
	minimum, minimumOK := optionalSchemaInteger(schema, "minimum", -9007199254740991)
	maximum, maximumOK := optionalSchemaInteger(schema, "maximum", 9007199254740991)
	return minimumOK && maximumOK && minimum <= maximum
}

func validIntegerTargetOptionsValue(value any, schema map[string]any) bool {
	integer, integerOK := schemaInteger(value)
	minimum, minimumOK := optionalSchemaInteger(schema, "minimum", -9007199254740991)
	maximum, maximumOK := optionalSchemaInteger(schema, "maximum", 9007199254740991)
	return integerOK && minimumOK && maximumOK && integer >= minimum && integer <= maximum
}

func validObjectTargetOptionsSchema(schema map[string]any, depth int) bool {
	if !ExactMap(schema, "additionalProperties", "properties", "required", "type") ||
		schema["additionalProperties"] != false {
		return false
	}
	properties, ok := schema["properties"].(map[string]any)
	required, requiredOK := schemaStringSet(schema["required"], true)
	if !ok || !requiredOK || len(properties) > 64 {
		return false
	}
	for name, property := range properties {
		child, childOK := property.(map[string]any)
		if !targetOptionsPropertyPattern.MatchString(name) || !childOK ||
			!validTargetOptionsSchema(child, depth+1, false) {
			return false
		}
	}
	for _, name := range required {
		if _, exists := properties[name]; !exists {
			return false
		}
	}
	return true
}

func validObjectTargetOptionsValue(value any, schema map[string]any, depth int) bool {
	object, objectOK := value.(map[string]any)
	properties, propertiesOK := schema["properties"].(map[string]any)
	required, requiredOK := schemaStringSet(schema["required"], true)
	if !objectOK || !propertiesOK || !requiredOK {
		return false
	}
	for _, name := range required {
		if _, exists := object[name]; !exists {
			return false
		}
	}
	for name, item := range object {
		property, exists := properties[name].(map[string]any)
		if !exists || !validTargetOptionsValue(item, property, depth+1) {
			return false
		}
	}
	return true
}

func validStringTargetOptionsSchema(schema map[string]any) bool {
	if !mapKeysAllowed(schema, "enum", "format", "maxLength", "minLength", "type") {
		return false
	}
	minimum, minimumOK := optionalNonNegativeSchemaInteger(schema, "minLength", 0)
	maximum, maximumOK := optionalNonNegativeSchemaInteger(schema, "maxLength", 4096)
	if !minimumOK || !maximumOK || maximum > 4096 || minimum > maximum ||
		schema["format"] != nil && schema["format"] != "safe-path" {
		return false
	}
	if values, exists := schema["enum"]; exists {
		enum, enumOK := schemaStringSet(values, false)
		return enumOK && len(enum) <= 64
	}
	return true
}

func validStringTargetOptionsValue(value any, schema map[string]any) bool {
	text, textOK := value.(string)
	minimum, minimumOK := optionalNonNegativeSchemaInteger(schema, "minLength", 0)
	maximum, maximumOK := optionalNonNegativeSchemaInteger(schema, "maxLength", 4096)
	length := int64(utf8.RuneCountInString(text))
	if !textOK || !utf8.ValidString(text) || !minimumOK || !maximumOK || length < minimum || length > maximum ||
		schema["format"] == "safe-path" && !SafePath(text) {
		return false
	}
	if values, exists := schema["enum"]; exists {
		enum, enumOK := schemaStringSet(values, false)
		return enumOK && containsString(enum, text)
	}
	return true
}

func validTargetOptionsSchema(schema map[string]any, depth int, root bool) bool {
	if depth > 8 {
		return false
	}
	baseType, nullable, ok := targetOptionsSchemaType(schema["type"])
	if !ok || root && (baseType != "object" || nullable) {
		return false
	}
	switch baseType {
	case "object":
		return validObjectTargetOptionsSchema(schema, depth)
	case "array":
		return validArrayTargetOptionsSchema(schema, depth)
	case "string":
		return validStringTargetOptionsSchema(schema)
	case "integer":
		return validIntegerTargetOptionsSchema(schema)
	case "boolean":
		return ExactMap(schema, "type")
	}
	return false
}

func validTargetOptionsValue(value any, schema map[string]any, depth int) bool {
	if depth > 8 {
		return false
	}
	baseType, nullable, ok := targetOptionsSchemaType(schema["type"])
	if !ok || value == nil {
		return ok && nullable && value == nil
	}
	switch baseType {
	case "object":
		return validObjectTargetOptionsValue(value, schema, depth)
	case "array":
		return validArrayTargetOptionsValue(value, schema, depth)
	case "string":
		return validStringTargetOptionsValue(value, schema)
	case "integer":
		return validIntegerTargetOptionsValue(value, schema)
	case "boolean":
		_, booleanOK := value.(bool)
		return booleanOK
	}
	return false
}
