package libraryimport

import (
	"encoding/json"

	application "retrom/internal/service/libraryimport"
)

func legacyNullable[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

func legacyDiagnostics(raw json.RawMessage) any {
	var result any
	_ = json.Unmarshal(raw, &result)
	return result
}

func legacyArcadeAttachment(value *application.ArcadeAttachment) any {
	if value == nil {
		return nil
	}
	return map[string]any{
		"attachmentId": value.ID, "machine": value.Machine, "expectedLogicalName": value.ExpectedLogicalName,
		"originalFilename": value.OriginalFilename, "state": value.State, "errorCode": legacyNullable(value.ErrorCode),
		"jobId": value.JobID, "observedSizeBytes": legacyNullable(value.ObservedSizeBytes),
		"observedSha256": legacyNullable(value.ObservedSHA256), "diagnostics": legacyDiagnostics(value.Diagnostics),
		"createdAtMs": value.CreatedAtMS, "updatedAtMs": value.UpdatedAtMS,
		"finishedAtMs": legacyNullable(value.FinishedAtMS),
	}
}

func legacyMultiDiscAttachment(value *application.MultiDiscAttachment) any {
	if value == nil {
		return nil
	}
	return map[string]any{
		"attachmentId": value.ID, "state": value.State, "errorCode": legacyNullable(value.ErrorCode),
		"diagnostics": legacyDiagnostics(value.Diagnostics), "jobId": value.JobID, "jobState": value.JobState,
		"version": value.Version, "jobVersion": value.JobVersion, "canRetry": value.CanRetry,
		"createdAtMs": value.CreatedAtMS, "updatedAtMs": value.UpdatedAtMS,
		"finishedAtMs": legacyNullable(value.FinishedAtMS),
	}
}

func legacyArcadeProjection(value *application.ReviewArcade) map[string]any {
	nodes := make([]map[string]any, 0, len(value.Nodes))
	for _, node := range value.Nodes {
		projected := map[string]any{
			"kind":                node.Kind,
			"machine":             node.Machine,
			"requiredBy":          node.RequiredBy,
			"depth":               node.Depth,
			"expectedLogicalName": node.ExpectedLogicalName,
			"state":               node.State,
			"requiredEntryCount":  node.RequiredEntryCount,
			"requiredEntries":     node.RequiredEntries,
			"canAttach":           node.CanAttach,
			"attachment":          legacyArcadeAttachment(node.Attachment),
		}
		if node.Kind == "BIOS_OR_BASE" {
			projected["managementUrl"] = "/admin/bios"
		}
		nodes = append(nodes, projected)
	}
	return map[string]any{
		"machine":           value.Machine,
		"status":            value.Status,
		"compatibilityCode": value.CompatibilityCode,
		"nodes":             nodes,
		"activeAttachment":  legacyArcadeAttachment(value.ActiveAttachment),
	}
}

func legacyMultiDiscProjection(value *application.ReviewMultiDisc) map[string]any {
	entries := make([]map[string]any, 0, len(value.Entries))
	for _, entry := range value.Entries {
		entries = append(entries,
			map[string]any{
				"index":           entry.Index,
				"discIndex":       entry.DiscIndex,
				"label":           entry.Label,
				"sourceReference": entry.SourceReference,
				"canonicalName":   entry.CanonicalName,
				"state":           entry.State,
				"logicalName":     legacyNullable(entry.LogicalName),
				"sizeBytes":       legacyNullable(entry.SizeBytes),
				"sha256":          legacyNullable(entry.SHA256),
			})
	}
	return map[string]any{
		"contentKind": value.ContentKind,
		"playlist": map[string]any{
			"name":      value.Playlist.Name,
			"sizeBytes": value.Playlist.SizeBytes,
			"sha256":    value.Playlist.SHA256,
		},
		"discCount":             value.DiscCount,
		"presentDiscCount":      value.PresentDiscCount,
		"missingDiscCount":      value.MissingDiscCount,
		"totalPresentBytes":     value.TotalPresentBytes,
		"maxDiscs":              value.MaxDiscs,
		"maxTotalBytes":         value.MaxTotalBytes,
		"entries":               entries,
		"missingReferences":     value.MissingReferences,
		"latestAttachment":      legacyMultiDiscAttachment(value.LatestAttachment),
		"activeAttachment":      legacyMultiDiscAttachment(value.ActiveAttachment),
		"canAttachMissingDiscs": value.CanAttachMissingDiscs,
	}
}
