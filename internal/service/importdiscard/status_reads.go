package importdiscard

import "context"

// GetMany reads the visible page in one snapshot, independently of import execution readers.
func (service *Service) GetMany(ctx context.Context, kind string, ids []string) (map[string]Status, error) {
	result := make(map[string]Status, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > 20 {
		return nil, ErrInvalid
	}
	for _, id := range ids {
		if !validKey(Key{kind, id}) {
			return nil, ErrInvalid
		}
	}
	err := service.repository.WithRead(ctx, func(records Reader) error {
		facts, err := records.StatusFacts(ctx, kind, ids)
		if err != nil {
			return failure("read batch discard facts", err)
		}
		for _, id := range ids {
			fact, found := facts[id]
			if !found {
				return ErrNotFound
			}
			result[id] = resolveStatus(Key{kind, id}, fact.Batch, fact.Disposition)
		}
		return nil
	})
	return result, failure("read batch discard statuses", err)
}

func resolveStatus(key Key, batch Batch, disposition *Disposition) Status {
	result := Status{Kind: key.Kind, ImportID: key.ID, State: "UNAVAILABLE"}
	if available(key.Kind, batch) {
		result.State = "AVAILABLE"
	}
	if disposition != nil {
		result.State = disposition.State
		result.ErrorCode = disposition.ErrorCode
	}
	return result
}
