package tagging

// ValidateCreateAdmission checks capacity and name uniqueness from already-loaded facts.
// Returns nil if the create may proceed.
func ValidateCreateAdmission(activeByKey map[string]string, nameKey string) error {
	if len(activeByKey) >= MaxActiveTags {
		return ErrLimitReached
	}
	if activeByKey[nameKey] != "" {
		return ErrNameConflict
	}
	return nil
}

// ValidateRenameAdmission checks preconditions for a rename from loaded tag state.
func ValidateRenameAdmission(
	tag AdminItem,
	expectedVersion int64,
	newName string,
	activeByKey map[string]string,
	newNameKey string,
) error {
	if tag.Status == StatusDeleted {
		return ErrAlreadyDeleted
	}
	if tag.Version != expectedVersion {
		return ErrVersionConflict
	}
	if tag.Name == newName {
		return ErrInvalid
	}
	if existingID := activeByKey[newNameKey]; existingID != "" && existingID != tag.TagID {
		return ErrNameConflict
	}
	return nil
}

// ValidateDeleteAdmission checks preconditions for a soft delete from loaded tag state.
func ValidateDeleteAdmission(tag AdminItem, expectedVersion int64, confirmName string) error {
	if tag.Status == StatusDeleted {
		return ErrAlreadyDeleted
	}
	if tag.Version != expectedVersion {
		return ErrVersionConflict
	}
	if confirmName != tag.Name {
		return ErrDeleteConfirmation
	}
	return nil
}

// ValidateEnsureCommonAdmission checks that adding missingCount new tags
// would not exceed the capacity.
func ValidateEnsureCommonAdmission(activeCount, missingCount int) error {
	if activeCount+missingCount > MaxActiveTags {
		return ErrLimitReached
	}
	return nil
}

// CommonTagNames returns the administrator-editable starter taxonomy in its stable display order.
func CommonTagNames() []string {
	names := [...]string{
		"动作冒险",
		"飞行射击",
		"格斗对战",
		"角色扮演",
		"模拟经营",
		"即时战略",
		"体育竞技",
		"益智解谜",
		"光枪射击",
		"生存恐怖",
	}
	result := make([]string, len(names))
	copy(result, names[:])
	return result
}
