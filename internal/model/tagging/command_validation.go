package tagging

// ValidateCreateCommand checks internal consistency of a CreateCommand.
// The repo calls this before applying the command.
func ValidateCreateCommand(cmd CreateCommand) error {
	if !ValidID(cmd.TagID) || !ValidID(cmd.AuditID) || !ValidID(cmd.ActorUserID) {
		return ErrInvalid
	}
	if cmd.NowMS <= 0 {
		return ErrInvalid
	}
	return validateNameFields(cmd.Name, cmd.NameKey, cmd.SearchText)
}

// ValidateRenameCommand checks internal consistency of a RenameCommand.
func ValidateRenameCommand(cmd RenameCommand) error {
	if !ValidID(cmd.TagID) || !ValidID(cmd.AuditID) || !ValidID(cmd.ActorUserID) {
		return ErrInvalid
	}
	if cmd.ExpectedVersion <= 0 || cmd.NowMS <= 0 {
		return ErrInvalid
	}
	return validateNameFields(cmd.Name, cmd.NameKey, cmd.SearchText)
}

// ValidateDeleteCommand checks internal consistency of a DeleteCommand.
func ValidateDeleteCommand(cmd DeleteCommand) error {
	if !ValidID(cmd.TagID) || !ValidID(cmd.AuditID) || !ValidID(cmd.ActorUserID) {
		return ErrInvalid
	}
	if cmd.ExpectedVersion <= 0 || cmd.NowMS <= 0 {
		return ErrInvalid
	}
	if cmd.ConfirmName == "" {
		return ErrInvalid
	}
	return nil
}

// ValidateReplaceGameTagsCommand checks internal consistency.
func ValidateReplaceGameTagsCommand(cmd ReplaceGameTagsCommand) error {
	if !ValidID(cmd.GameID) || !ValidID(cmd.AuditID) || !ValidID(cmd.ActorUserID) {
		return ErrInvalid
	}
	if cmd.ExpectedVersion <= 0 || cmd.NowMS <= 0 {
		return ErrInvalid
	}
	if _, err := ValidateIDs(cmd.TagIDs); err != nil {
		return err
	}
	return nil
}

// ValidateEnsureCommonTagsCommand checks internal consistency.
func ValidateEnsureCommonTagsCommand(cmd EnsureCommonTagsCommand) error {
	if !ValidID(cmd.ActorUserID) {
		return ErrInvalid
	}
	if cmd.NowMS <= 0 {
		return ErrInvalid
	}
	for _, c := range cmd.Candidates {
		if !ValidID(c.TagID) || !ValidID(c.AuditID) {
			return ErrInvalid
		}
		if err := validateNameFields(c.Name, c.NameKey, c.SearchText); err != nil {
			return err
		}
	}
	return nil
}

// validateNameFields checks that Name, NameKey and SearchText are consistent
// with the output of NormalizeName.
func validateNameFields(name, nameKey, searchText string) error {
	if name == "" || nameKey == "" || searchText == "" {
		return ErrNameInvalid
	}
	expectedDisplay, expectedKey, expectedSearch, err := NormalizeName(name)
	if err != nil {
		return err
	}
	if name != expectedDisplay || nameKey != expectedKey || searchText != expectedSearch {
		return ErrNameInvalid
	}
	return nil
}
