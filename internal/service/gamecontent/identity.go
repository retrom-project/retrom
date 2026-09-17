package gamecontent

import (
	"retrom/internal/capability/content/multidisc"
	model "retrom/internal/model/gamecontent"
)

func preparedContentIdentity(prepared model.PreparedReplacement) []model.IdentityFile {
	if prepared.ContentKind == multidisc.ContentKind {
		identity := make([]model.IdentityFile, 0, len(prepared.OrderedDiscSHA256))
		for _, digest := range prepared.OrderedDiscSHA256 {
			identity = append(identity, model.IdentityFile{Role: "DISC", SHA256: digest})
		}
		return identity
	}
	identity := make([]model.IdentityFile, 0, len(prepared.Files))
	for _, file := range prepared.Files {
		identity = append(identity, model.IdentityFile{Role: file.Role, SHA256: file.SHA256})
	}
	return identity
}
