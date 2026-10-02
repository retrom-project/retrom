package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/service/accounts"
)

type (
	LinkRepository struct{ reader, writer dbapi.DB }
	linkRecords    struct{ accountOperations }
)

func NewLinks(reader, writer dbapi.DB) *LinkRepository {
	return &LinkRepository{reader: reader, writer: writer}
}

func (repository *LinkRepository) Current(ctx context.Context, id string) (accounts.LinkRecord, bool, error) {
	return (linkRecords{accountOperations{repository.reader}}).Current(ctx, id)
}

func (repository *LinkRepository) WithWrite(ctx context.Context, work func(accounts.LinkScope) error) error {
	tx, err := repository.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin account link change: %w", err)
	}
	defer dbapi.Rollback(tx)
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
	record, err := scanAccountLink(dbapi.QueryRowContext(
		ctx, records.executor, accountLinkProjection+` WHERE link.id=?`, id))
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
	rows, err := repository.reader.QueryContext(ctx, statement, arguments...)
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

func scanAccountLink(scanner dbapi.Scanner) (accounts.LinkRecord, error) {
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
