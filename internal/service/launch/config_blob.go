package launch

import "strings"

func providerBlobResource(
	source ConfigSource,
	kind string,
	files []ConfigFile,
) (map[string]any, error) {
	selected := ConfigFile{}
	for _, file := range files {
		if kind == "SEEKABLE_BLOB" && file.LogicalName == MKXPArchiveName {
			selected = file
			break
		}
		if (kind != "SEEKABLE_BLOB" || source.ContentKind == "SINGLE_FILE") &&
			!strings.HasPrefix(file.LogicalName, "__retrom__/") {
			selected = file
			break
		}
	}
	if selected.LogicalName == "" || selected.Size < 1 {
		return nil, ErrCredential
	}
	identity, err := ContentIdentity(ContentView{
		Digest: selected.Digest, Format: selected.Format, CoreID: source.CoreID,
		ProviderID: source.ProviderID, TargetID: source.TargetID,
		BundleSHA256: source.BundleDigest,
		DOSEntry:     source.DOSEntry,
	})
	if err != nil {
		return nil, err
	}
	url, err := RuntimeContentURL("game", identity, selected.LogicalName)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"kind": kind, "url": url, "sha256": selected.Digest, "sizeBytes": selected.Size,
		"rangeRequired": kind == "SEEKABLE_BLOB",
	}, nil
}

func providerSeekableProjectResource(
	projectIdentity string,
	files []ConfigFile,
) (map[string]any, error) {
	selected := ConfigFile{}
	for _, file := range files {
		if file.LogicalName == MKXPArchiveName {
			selected = file
			break
		}
	}
	if selected.LogicalName == "" || selected.Size < 1 || !validContentDigest(selected.Digest) {
		return nil, ErrCredential
	}
	root, err := RuntimeProjectContentRoot(projectIdentity)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"kind": "SEEKABLE_BLOB", "url": root + MKXPArchivePublicName,
		"sha256": selected.Digest, "sizeBytes": selected.Size, "rangeRequired": true,
	}, nil
}
