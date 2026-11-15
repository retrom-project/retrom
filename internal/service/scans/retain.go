package scans

import (
	"encoding/json"
	"fmt"

	"retrom/internal/model"
)

// Content kinds whose only reviewer choice is the entry file. ARCADE couples its
// entry with ROM-set facts and is configured again from the new files.
var retainedEntryKinds = map[string]bool{
	"SINGLE_FILE": true, "ONS_PROJECT": true, "KIRIKIRI_PROJECT": true, "BUTTERSCOTCH_PROJECT": true,
	"TYRANOSCRIPT_PROJECT": true, "NXENGINE_PROJECT": true, "DAPHNE_PROJECT": true,
	"BGI_PROJECT": true, "SIGLUS_PROJECT": true,
}

type rawObject = map[string]json.RawMessage

// retainReviewerSelections keeps the saved entry and core options across content
// replacement when the new content has the same kind and still contains the entry.
// The caller falls back to the plain new configuration if runtime rejects the result.
func retainReviewerSelections(previous, next json.RawMessage, files []model.ContentFile) (json.RawMessage, error) {
	var oldConfig, newConfig, oldContent, newContent rawObject
	if err := decodeObjects(previous, &oldConfig, next, &newConfig); err != nil {
		return nil, err
	}
	if err := decodeObjects(oldConfig["content"], &oldContent, newConfig["content"], &newContent); err != nil {
		return nil, err
	}
	if string(oldContent["kind"]) != string(newContent["kind"]) {
		return next, nil
	}
	entryChanged := retainEntry(oldContent, newContent, files)
	cores, coresChanged, err := retainCoreOptions(oldConfig["cores"], newConfig["cores"])
	if err != nil || !entryChanged && !coresChanged {
		return next, err
	}
	if newConfig["content"], err = json.Marshal(newContent); err != nil {
		return nil, fmt.Errorf("encode replacement content: %w", err)
	}
	if coresChanged {
		newConfig["cores"] = cores
	}
	config, err := json.Marshal(newConfig)
	if err != nil {
		return nil, fmt.Errorf("encode replacement configuration: %w", err)
	}
	return config, nil
}

func retainEntry(oldContent, newContent rawObject, files []model.ContentFile) bool {
	var kind, entry string
	if json.Unmarshal(oldContent["kind"], &kind) != nil || !retainedEntryKinds[kind] ||
		len(oldContent) != 2 || len(newContent) != 2 ||
		json.Unmarshal(oldContent["entryFile"], &entry) != nil || !containsLogicalKey(files, entry) {
		return false
	}
	newContent["entryFile"] = oldContent["entryFile"]
	return true
}

// Saved options override the same keys from the new configuration; new keys stay.
// File references (parentFiles, romSet) always come from the new configuration.
func retainCoreOptions(previous, next json.RawMessage) (json.RawMessage, bool, error) {
	if len(previous) == 0 {
		return next, false, nil
	}
	var oldCores map[string]rawObject
	if err := json.Unmarshal(previous, &oldCores); err != nil {
		return nil, false, fmt.Errorf("read saved core options: %w", err)
	}
	newCores := map[string]rawObject{}
	if len(next) != 0 {
		if err := json.Unmarshal(next, &newCores); err != nil {
			return nil, false, fmt.Errorf("read replacement core options: %w", err)
		}
	}
	changed := false
	for id, core := range oldCores {
		merged, saved, err := mergeCoreOptions(core["options"], newCores[id])
		if err != nil {
			return nil, false, err
		}
		if saved {
			newCores[id], changed = merged, true
		}
	}
	if !changed {
		return next, false, nil
	}
	encoded, err := json.Marshal(newCores)
	if err != nil {
		return nil, false, fmt.Errorf("encode core options: %w", err)
	}
	return encoded, true, nil
}

// mergeCoreOptions reports false when nothing was saved for this core.
func mergeCoreOptions(saved json.RawMessage, target rawObject) (rawObject, bool, error) {
	var savedOptions rawObject
	if len(saved) == 0 {
		return target, false, nil
	}
	if err := json.Unmarshal(saved, &savedOptions); err != nil {
		return nil, false, fmt.Errorf("read saved core options: %w", err)
	}
	if len(savedOptions) == 0 {
		return target, false, nil
	}
	if target == nil {
		target = rawObject{}
	}
	options := rawObject{}
	if len(target["options"]) != 0 {
		if err := json.Unmarshal(target["options"], &options); err != nil {
			return nil, false, fmt.Errorf("read replacement core options: %w", err)
		}
	}
	for key, value := range savedOptions {
		options[key] = value
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return nil, false, fmt.Errorf("encode core options: %w", err)
	}
	target["options"] = encoded
	return target, true, nil
}

func decodeObjects(firstRaw json.RawMessage, first any, secondRaw json.RawMessage, second any) error {
	if err := json.Unmarshal(firstRaw, first); err != nil {
		return fmt.Errorf("read saved replacement configuration: %w", err)
	}
	if err := json.Unmarshal(secondRaw, second); err != nil {
		return fmt.Errorf("read replacement configuration: %w", err)
	}
	return nil
}

func containsLogicalKey(files []model.ContentFile, key string) bool {
	for _, file := range files {
		if file.LogicalKey == key {
			return true
		}
	}
	return false
}
