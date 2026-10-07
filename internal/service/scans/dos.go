package scans

import (
	"encoding/json"
	"fmt"
)

// Keep the user's DOS program choice across content replacement. An obsolete path
// remains explicit so candidate inspection can flag it even after a page reload.
func retainDOSProgram(previous, next json.RawMessage) (json.RawMessage, error) {
	var oldConfig, newConfig map[string]json.RawMessage
	var oldContent, newContent map[string]json.RawMessage
	for _, item := range []struct {
		raw    json.RawMessage
		target any
	}{
		{previous, &oldConfig}, {next, &newConfig},
	} {
		if err := json.Unmarshal(item.raw, item.target); err != nil {
			return nil, fmt.Errorf("read replacement configuration: %w", err)
		}
	}
	for _, item := range []struct {
		raw    json.RawMessage
		target any
	}{
		{oldConfig["content"], &oldContent}, {newConfig["content"], &newContent},
	} {
		if err := json.Unmarshal(item.raw, item.target); err != nil {
			return nil, fmt.Errorf("read replacement content: %w", err)
		}
	}
	if string(oldContent["kind"]) != `"DOS_BUNDLE"` || string(newContent["kind"]) != `"DOS_BUNDLE"` {
		return next, nil
	}
	delete(newContent, "entryPath")
	if selected, exists := oldContent["entryPath"]; exists {
		newContent["entryPath"] = selected
	}
	content, err := json.Marshal(newContent)
	if err != nil {
		return nil, fmt.Errorf("encode DOS content: %w", err)
	}
	newConfig["content"] = content
	config, err := json.Marshal(newConfig)
	if err != nil {
		return nil, fmt.Errorf("encode DOS configuration: %w", err)
	}
	return config, nil
}
