package runtimebundle

import (
	"encoding/json"
	"regexp"
	"sort"
	"unicode/utf8"
)

const maxTargetOptionsBytes = 16 * 1024

var targetOptionsPropertyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,63}$`)

// TargetOptionsSchema is the deliberately small Provider-owned schema dialect
// embedded in every target contract. It is JSON Schema shaped, but only the
// closed subset validated below is part of Provider API 1.
type TargetOptionsSchema map[string]any

func (schema *TargetOptionsSchema) UnmarshalJSON(contents []byte) error {
	value, err := parseStrictJSON(contents)
	object, ok := value.(map[string]any)
	if err != nil || !ok || !validTargetOptionsSchema(object, 0, true) {
		return ErrManifestInvalid
	}
	*schema = object
	return nil
}

// ValidateTargetOptions applies a target's exact Provider-owned schema after
// enforcing the generic JSON depth, fan-out, and encoded-size boundary.
func ValidateTargetOptions(schema TargetOptionsSchema, value map[string]any) bool {
	if schema == nil || value == nil || !validJSONValue(value, 0, true) {
		return false
	}
	contents, err := json.Marshal(value)
	return err == nil && len(contents) <= maxTargetOptionsBytes &&
		validTargetOptionsValue(value, schema, 0)
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
		return exactMap(schema, "type")
	}
	return false
}

func validObjectTargetOptionsSchema(schema map[string]any, depth int) bool {
	if !exactMap(schema, "additionalProperties", "properties", "required", "type") ||
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

func validIntegerTargetOptionsSchema(schema map[string]any) bool {
	if !mapKeysAllowed(schema, "maximum", "minimum", "type") {
		return false
	}
	minimum, minimumOK := optionalSchemaInteger(schema, "minimum", -9007199254740991)
	maximum, maximumOK := optionalSchemaInteger(schema, "maximum", 9007199254740991)
	return minimumOK && maximumOK && minimum <= maximum
}

func validTargetOptionsValue(value any, schema TargetOptionsSchema, depth int) bool {
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

func validObjectTargetOptionsValue(value any, schema TargetOptionsSchema, depth int) bool {
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
		if !exists || !validTargetOptionsValue(item, TargetOptionsSchema(property), depth+1) {
			return false
		}
	}
	return true
}

func validArrayTargetOptionsValue(value any, schema TargetOptionsSchema, depth int) bool {
	items, arrayOK := value.([]any)
	itemSchema, itemOK := schema["items"].(map[string]any)
	minimum, minimumOK := optionalNonNegativeSchemaInteger(schema, "minItems", 0)
	maximum, maximumOK := optionalNonNegativeSchemaInteger(schema, "maxItems", 0)
	if !arrayOK || !itemOK || !minimumOK || !maximumOK ||
		int64(len(items)) < minimum || int64(len(items)) > maximum {
		return false
	}
	for _, item := range items {
		if !validTargetOptionsValue(item, TargetOptionsSchema(itemSchema), depth+1) {
			return false
		}
	}
	return true
}

func validStringTargetOptionsValue(value any, schema TargetOptionsSchema) bool {
	text, textOK := value.(string)
	minimum, minimumOK := optionalNonNegativeSchemaInteger(schema, "minLength", 0)
	maximum, maximumOK := optionalNonNegativeSchemaInteger(schema, "maxLength", 4096)
	length := int64(utf8.RuneCountInString(text))
	if !textOK || !utf8.ValidString(text) || !minimumOK || !maximumOK || length < minimum || length > maximum ||
		schema["format"] == "safe-path" && !safePath(text) {
		return false
	}
	if values, exists := schema["enum"]; exists {
		enum, enumOK := schemaStringSet(values, false)
		return enumOK && containsString(enum, text)
	}
	return true
}

func validIntegerTargetOptionsValue(value any, schema TargetOptionsSchema) bool {
	integer, integerOK := schemaInteger(value)
	minimum, minimumOK := optionalSchemaInteger(schema, "minimum", -9007199254740991)
	maximum, maximumOK := optionalSchemaInteger(schema, "maximum", 9007199254740991)
	return integerOK && minimumOK && maximumOK && integer >= minimum && integer <= maximum
}

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

func targetOptionsBaseType(value string) bool {
	return value == "array" || value == "boolean" || value == "integer" || value == "object" || value == "string"
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

func uniqueStrings(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] == values[index] {
			return false
		}
	}
	return true
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

func nonNegativeSchemaInteger(value any) (int64, bool) {
	result, ok := schemaInteger(value)
	return result, ok && result >= 0
}

func optionalSchemaInteger(value map[string]any, key string, fallback int64) (int64, bool) {
	candidate, exists := value[key]
	if !exists {
		return fallback, true
	}
	return schemaInteger(candidate)
}

func optionalNonNegativeSchemaInteger(value map[string]any, key string, fallback int64) (int64, bool) {
	candidate, exists := value[key]
	if !exists {
		return fallback, true
	}
	return nonNegativeSchemaInteger(candidate)
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
