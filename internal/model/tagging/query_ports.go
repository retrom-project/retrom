package tagging

import "context"

// QueryRepository provides read-only access to the tagging aggregate.
// It replaces the read methods previously on the monolithic Repository.
type QueryRepository interface {
	Get(context.Context, string) (AdminItem, error)
	List(context.Context, ListQuery) ([]AdminItem, error)
	Summary(context.Context) (Summary, error)
	References(context.Context, OwnerKind, []string) (map[string][]Reference, error)
}

// CommandRepository provides named atomic write commands. Each method
// opens its own short BEGIN IMMEDIATE transaction, reads current facts,
// applies pure policies and commits. The service never holds a
// transaction or receives a WriteScope.
type CommandRepository interface {
	CommitCreate(context.Context, CreateCommand) (AdminItem, error)
	CommitRename(context.Context, RenameCommand) (AdminItem, error)
	CommitDelete(context.Context, DeleteCommand) (AdminItem, DeleteImpact, error)
	CommitReplaceGameTags(context.Context, ReplaceGameTagsCommand) (GameTagResult, error)
	CommitEnsureCommonTags(context.Context, EnsureCommonTagsCommand) (CommonTagsResult, error)
}
