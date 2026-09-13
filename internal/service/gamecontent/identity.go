package gamecontent

import "retrom/internal/capability/content/multidisc"

func preparedContentIdentity(prepared PreparedReplacement) []IdentityFile {
	if prepared.ContentKind == multidisc.ContentKind {
		identity := make([]IdentityFile, 0, len(prepared.OrderedDiscSHA256))
		for _, digest := range prepared.OrderedDiscSHA256 {
			identity = append(identity, IdentityFile{Role: "DISC", SHA256: digest})
		}
		return identity
	}
	identity := make([]IdentityFile, 0, len(prepared.Files))
	for _, file := range prepared.Files {
		identity = append(identity, IdentityFile{Role: file.Role, SHA256: file.SHA256})
	}
	return identity
}
