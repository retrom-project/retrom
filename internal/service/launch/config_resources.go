package launch

import (
	"encoding/json"
	"errors"

	model "retrom/internal/model/launch"
	runtimecontract "retrom/internal/model/runtimecontract"
)

var errConfigInputMissing = errors.New("launch input absent")

func providerResources(
	snapshot model.ConfigSnapshot,
	target runtimecontract.Target,
	ticket model.IsolationTicket,
) ([]json.RawMessage, error) {
	resources := make([]json.RawMessage, 0, len(target.Inputs))
	for _, input := range target.Inputs {
		resource, err := providerInputResource(snapshot, input, ticket)
		if errors.Is(err, errConfigInputMissing) && input.Optional {
			continue
		}
		if errors.Is(err, errConfigInputMissing) {
			return nil, model.ErrCredential
		}
		if err != nil {
			return nil, err
		}
		resource["role"], resource["ordinal"] = input.Role, 0
		encoded, err := encodeProviderValue(resource)
		if err != nil {
			return nil, err
		}
		resources = append(resources, encoded)
	}
	return resources, nil
}

func providerInputResource(
	snapshot model.ConfigSnapshot,
	input runtimecontract.Input,
	ticket model.IsolationTicket,
) (map[string]any, error) {
	source := snapshot.Authority.Source
	switch input.Role {
	case "game":
		return providerGameResource(source, input.Kind, configFilesWithRole(snapshot.Files, "GAME"), ticket)
	case "bios":
		return providerBundleResource(configFilesWithRole(snapshot.Files, "BIOS_BUNDLE"), input.Kind)
	case "parent":
		return providerParentResource(configFilesWithRole(snapshot.Files, "PARENT"), input.Kind)
	case "external":
		return providerExternalResource(configFilesWithRole(snapshot.Files, "EXTERNAL_FILE"), input.Kind)
	case "discs":
		return providerDiscResource(configFilesWithRole(snapshot.Files, "DISC"), source.InitialDisc, input.Kind)
	case "rtp":
		// RPG launches intentionally do not mount historical RTP selections.
		if source.ContentKind == "RPG_MAKER_PROJECT" {
			return nil, errConfigInputMissing
		}
		return nil, model.ErrCredential
	default:
		return nil, model.ErrCredential
	}
}

func configFilesWithRole(files []model.ConfigFile, role string) []model.ConfigFile {
	result := make([]model.ConfigFile, 0)
	for _, file := range files {
		if file.Role == role {
			result = append(result, file)
		}
	}
	return result
}

func providerGameResource(
	source model.ConfigSource,
	kind string,
	files []model.ConfigFile,
	ticket model.IsolationTicket,
) (map[string]any, error) {
	if len(files) == 0 {
		return nil, errConfigInputMissing
	}
	switch kind {
	case "FILE_TREE":
		identity, err := ProjectIdentity(files)
		if err != nil {
			return nil, err
		}
		root, err := RuntimeProjectContentRoot(identity)
		if err != nil {
			return nil, err
		}
		return map[string]any{"kind": kind, "indexUrl": root + "index.json", "contentDigest": identity}, nil
	case "SEEKABLE_BLOB":
		if source.ContentKind == "SINGLE_FILE" {
			return providerBlobResource(source, kind, files)
		}
		identity, err := ProjectIdentity(files)
		if err != nil {
			return nil, err
		}
		return providerSeekableProjectResource(identity, files)
	case "NATIVE_WEB", "ISOLATED_WEB":
		return providerWebResource(source, kind, files, ticket)
	}
	return providerBlobResource(source, kind, files)
}

func providerWebResource(
	source model.ConfigSource,
	kind string,
	files []model.ConfigFile,
	ticket model.IsolationTicket,
) (map[string]any, error) {
	if ticket.Origin == "" || ticket.Ticket == "" {
		return nil, model.ErrBlocked
	}
	identity, err := ProjectIdentity(files)
	if err != nil {
		return nil, err
	}
	entry := ticket.Origin + "/__retrom/bootstrap"
	var cleanupURL *string
	if source.ContentKind == "TYRANOSCRIPT_PROJECT" {
		entry = ticket.Origin + "/__retrom/tyranoscript/bootstrap"
		value := ticket.Origin + "/__retrom/tyranoscript/cleanup"
		cleanupURL = &value
	}
	return map[string]any{
		"kind": kind, "origin": ticket.Origin, "entryUrl": entry, "bootstrapTicket": ticket.Ticket,
		"cleanupUrl": cleanupURL, "contentDigest": identity,
	}, nil
}
