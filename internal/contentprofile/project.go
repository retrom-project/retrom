package contentprofile

import "slices"

func project(platformID string, kind ContentKind, extraFormats ...ArchiveFormat) Profile {
	return Profile{
		PlatformID: platformID, ArchivePolicy: ArchiveProject,
		ArchiveFormats: append([]ArchiveFormat{ArchiveZIP, ArchiveSevenZip}, extraFormats...),
		FormatCode:     string(kind), ContentKinds: []ContentKind{kind},
	}
}

func ProjectKind(platformID string) (ContentKind, bool) {
	profile, ok := registry[platformID]
	if !ok || profile.ArchivePolicy != ArchiveProject || len(profile.ContentKinds) != 1 {
		return "", false
	}
	return profile.ContentKinds[0], true
}

func IsProjectContentKind(kind ContentKind) bool {
	for _, profile := range registry {
		if profile.ArchivePolicy == ArchiveProject && slices.Contains(profile.ContentKinds, kind) {
			return true
		}
	}
	return false
}

func projectExtensions(formats []ArchiveFormat) []string {
	result := make([]string, 0, len(formats))
	for _, format := range formats {
		switch format {
		case ArchiveZIP:
			result = append(result, ".zip")
		case ArchiveSevenZip:
			result = append(result, ".7z")
		case ArchiveNWJSExecutable:
			result = append(result, ".exe")
		case ArchiveElectronASAR:
			// ASAR is discovered inside an Electron project, not admitted as
			// a standalone game upload.
		}
	}
	return result
}
