package libraryimport

import (
	contentcapability "retrom/internal/content/capability"
	"retrom/internal/content/diagnostic"
)

// Validate the delivered bytes, after archive selection/materialization. The
// uploaded ZIP size is not the size consumed by a runtime's ROM input.
func applyPreparedInputLimits(plan *PreparedImport) error {
	return filterPreparedGroups(plan, func(group *PreparedGroup) (*diagnostic.Rejection, error) {
		rejection, rejected, err := preparedGroupLimit(plan.Target.Policy, *group, plan.Archives)
		if !rejected {
			return nil, err
		}
		return &rejection, err
	})
}

func filterPreparedGroups(plan *PreparedImport, check func(*PreparedGroup) (*diagnostic.Rejection, error)) error {
	accepted := make([]PreparedGroup, 0, len(plan.Groups))
	for _, group := range plan.Groups {
		rejection, err := check(&group)
		if err != nil {
			return err
		}
		if rejection == nil {
			accepted = append(accepted, group)
			continue
		}
		ids := make(map[string]bool, len(group.Sources))
		for _, source := range group.Sources {
			ids[source.File.ID] = true
		}
		for index := range plan.Dispositions {
			disposition := &plan.Dispositions[index]
			if ids[disposition.File.ID] {
				disposition.Disposition, disposition.Reason, disposition.Rejection = "REJECTED", rejection.Code, rejection
			}
		}
	}
	plan.Groups = accepted
	return nil
}

func preparedGroupLimit(policy contentcapability.Policy, group PreparedGroup, archives []PreparedArchive,
) (diagnostic.Rejection, bool, error) {
	kind := PreparedGroupContentKind(group)
	if policy.MaxFileBytes(kind) == 0 {
		return diagnostic.Rejection{}, false, nil
	}
	for _, source := range group.Sources {
		if source.Role == "PLAYLIST_SOURCE" {
			continue
		}
		_, size, err := preparedSourceIdentity(source, archives)
		if err != nil {
			return diagnostic.Rejection{}, false, err
		}
		if rejection := policy.CheckFile(kind, source.LogicalName, size); rejection != nil {
			return *rejection, true, nil
		}
	}
	return diagnostic.Rejection{}, false, nil
}
