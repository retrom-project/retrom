package tagging

import (
	"context"

	model "retrom/internal/model/tagging"
)

type writeScope struct {
	tags      tagReader
	changes   tagWriter
	relations relationWriter
	games     gameWriter
	audit     auditWriter
}

type tagReader interface {
	Get(context.Context, string) (model.AdminItem, error)
	ActiveByNameKey(context.Context) (map[string]string, error)
	ActiveReferences(context.Context, []string) ([]model.Reference, error)
}

type tagWriter interface {
	Insert(context.Context, model.TagWrite) error
	Rename(context.Context, model.TagWrite) error
	Delete(context.Context, model.TagWrite) error
}

type relationWriter interface {
	References(context.Context, model.Owner) ([]model.Reference, error)
	Add(context.Context, model.Assignment) error
	Remove(context.Context, model.Owner, []string) error
	TouchTags(context.Context, string, []string, int64) error
}

type gameWriter interface {
	Version(context.Context, string) (int64, error)
	Touch(context.Context, string, int64, int64) error
}

type auditWriter interface {
	Record(context.Context, model.AuditEvent) error
}
