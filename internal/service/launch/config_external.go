package launch

import (
	"fmt"
	model "retrom/internal/model/launch"
	"strings"
)

func providerBundleIdentity(files []model.ConfigFile) (string, error) {
	if len(files) == 0 {
		return "", errConfigInputMissing
	}
	members := make([]model.BundleFile, 0, len(files))
	for _, file := range files {
		members = append(members, model.BundleFile{LogicalName: file.LogicalName, SHA256: file.Digest})
	}
	return BundleIdentity(members)
}

func providerBundleResource(files []model.ConfigFile, kind string) (map[string]any, error) {
	if kind != "BIOS_BUNDLE" {
		return nil, model.ErrCredential
	}
	identity, err := providerBundleIdentity(files)
	if err != nil {
		return nil, err
	}
	url, err := RuntimeContentURL("bios", identity, "bundle.zip")
	if err != nil {
		return nil, err
	}
	return map[string]any{"kind": kind, "files": []map[string]any{{
		"logicalName": "bundle.zip", "virtualPath": "bundle.zip", "url": url,
		"sha256": identity, "sizeBytes": int64(len(files)),
	}}}, nil
}

func providerParentResource(files []model.ConfigFile, kind string) (map[string]any, error) {
	if kind != "PARENT_ARCHIVE" {
		return nil, model.ErrCredential
	}
	identity, err := providerBundleIdentity(files)
	if err != nil {
		return nil, err
	}
	url, err := RuntimeContentURL("parent", identity, "bundle.zip")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"kind": kind, "url": url, "sha256": identity, "sizeBytes": int64(len(files)), "rangeRequired": true,
	}, nil
}

func providerExternalResource(files []model.ConfigFile, kind string) (map[string]any, error) {
	if kind != "EXTERNAL_FILE_SET" {
		return nil, model.ErrCredential
	}
	if len(files) == 0 {
		return nil, errConfigInputMissing
	}
	result := make([]map[string]any, 0, len(files))
	for _, file := range files {
		identity, err := ExternalContentIdentity(file.Digest)
		if err != nil {
			return nil, err
		}
		url, err := RuntimeContentURL("external", identity, file.LogicalName)
		if err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"logicalName": file.LogicalName, "virtualPath": strings.TrimPrefix(file.VirtualPath, "/"),
			"url": url, "sha256": file.Digest, "sizeBytes": file.Size,
		})
	}
	return map[string]any{"kind": kind, "files": result}, nil
}

func providerDiscResource(files []model.ConfigFile, initial int64, kind string) (map[string]any, error) {
	if kind != "MULTI_DISC" {
		return nil, model.ErrCredential
	}
	if len(files) == 0 {
		return nil, errConfigInputMissing
	}
	if len(files) < 2 || initial < 0 || initial >= int64(len(files)) {
		return nil, model.ErrCredential
	}
	entries := make([]map[string]any, 0, len(files))
	for index, file := range files {
		identity, err := ExternalContentIdentity(file.Digest)
		if err != nil {
			return nil, err
		}
		url, err := RuntimeContentURL("external", identity, file.LogicalName)
		if err != nil {
			return nil, err
		}
		entries = append(entries, map[string]any{
			"index": index, "label": fmt.Sprintf("光盘 %d", index+1),
			"url": url, "sha256": file.Digest, "sizeBytes": file.Size,
		})
	}
	return map[string]any{"kind": kind, "initialDiscIndex": initial, "entries": entries}, nil
}
