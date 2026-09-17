package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/accounts"
	"retrom/internal/repo/dbexec"
)

type (
	LinkRepository struct{ database *sql.DB }
	linkRecords    struct{ accountOperations }
)

func NewLinks(database *sql.DB) *LinkRepository { return &LinkRepository{database} }
func (repository *LinkRepository) Current(ctx context.Context, id string) (accounts.LinkRecord, bool, error) {
	return (linkRecords{accountOperations{repository.database}}).Current(ctx, id)
}

func (repository *LinkRepository) CommitWrite(ctx context.Context, work func(accounts.LinkScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin account link change: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := linkRecords{accountOperations{tx}}
	if err := work(accounts.LinkScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account link change: %w", err)
	}
	return nil
}

const accountLinkProjection = `SELECT link.id,link.kind,link.invited_role,link.target_user_id,
 creator.id,creator.username,link.version,link.created_at_ms,link.expires_at_ms,
 link.consumed_at_ms,link.revoked_at_ms,target.username
 FROM account_links link JOIN users creator ON creator.id=link.created_by_user_id
 LEFT JOIN users target ON target.id=link.target_user_id`

func (records linkRecords) Current(ctx context.Context, id string) (accounts.LinkRecord, bool, error) {
	record, err := scanAccountLink(records.executor.QueryRowContext(ctx, accountLinkProjection+` WHERE link.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return record, false, nil
	}
	if err != nil {
		return record, false, err
	}
	return record, true, nil
}

func (repository *LinkRepository) List(ctx context.Context, query accounts.LinkQuery) ([]accounts.LinkRecord, error) {
	statement, arguments := buildLinkListQuery(query.Filter, query.Now)
	rows, err := repository.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query account links: %w", err)
	}
	defer func() { cleanup.Error("close account link rows", rows.Close()) }()
	records := make([]accounts.LinkRecord, 0, query.Filter.Limit)
	for rows.Next() {
		record, err := scanAccountLink(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account links: %w", err)
	}
	return records, nil
}

func scanAccountLink(scanner dbexec.Scanner) (accounts.LinkRecord, error) {
	var record accounts.LinkRecord
	var creator accounts.LinkCreator
	item := &record.Link
	err := scanner.Scan(
		&item.AccountLinkID,
		&item.Kind,
		&item.Role,
		&item.TargetUserID,
		&creator.UserID,
		&creator.Username,
		&item.Version,
		&item.CreatedAtMS,
		&item.ExpiresAtMS,
		&item.ConsumedAtMS,
		&item.RevokedAtMS,
		&record.TargetUsername,
	)
	if err != nil {
		return record, fmt.Errorf("scan account link: %w", err)
	}
	item.CreatedBy = &creator
	return record, nil
}
