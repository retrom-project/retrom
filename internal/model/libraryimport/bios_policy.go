package libraryimport

// BIOSContentLogicalName preserves the first content source and DOS entry fallback.
func (group PreparedGroup) BIOSContentLogicalName(platformID string) string {
	name := ""
	for _, source := range group.Sources {
		if source.Role == "CONTENT" || source.Role == "DISC" {
			name = source.LogicalName
			break
		}
	}
	if name == "" && platformID == "dos" {
		name = group.DefaultDOSEntry
	}
	return name
}
