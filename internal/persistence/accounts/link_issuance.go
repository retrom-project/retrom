package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/accounts"
)

func (repository *LinkRepository) WithIssueWrite(ctx context.Context, work func(accounts.LinkIssueScope) error) error {
	err := dbapi.RetryTransaction(ctx, repository.writer, func(tx dbapi.Tx) error {
		records := linkRecords{accountOperations{tx}}
		if err := work(accounts.LinkIssueScope{Read: records, Write: records}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit account link issuance: %w", err)
	}
	return nil
}

func (records linkRecords) Target(ctx context.Context, id string) (accounts.LinkTarget, bool, error) {
	var target accounts.LinkTarget
	err := dbapi.QueryRowContext(
		ctx, records.executor,

		`SELECT id,username,display_name,role,profile_id,status,version,session_version
FROM users WHERE id=?`,
		id,
	).Scan(
		&target.User.UserID,
		&target.User.Username,
		&target.User.DisplayName,
		&target.User.Role,
		&target.ProfileID,
		&target.Status,
		&target.Version,
		&target.SessionVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return target, false, nil
	}
	if err != nil {
		return target, false, fmt.Errorf("read account link target: %w", err)
	}
	return target, true, nil
}

func (records linkRecords) Issue(ctx context.Context, plan accounts.LinkIssuePlan) error {
	link := plan.Link
	if plan.Target != nil {
		result, err := recordstore.UpdateUsers(
			ctx,
			records.executor,
			recordstore.Update{
				Set: `version=version+1,updated_at_ms=?`,
				Scope: recordstore.Scope{
					Where: `id=? AND version=? AND status!='DELETED'`,
					Args: []any{
						plan.Target.User.UserID,
						plan.Target.Version,
					},
				},
				Values: []any{
					link.CreatedAtMS,
				},
			},
		)
		if err := authChanged(result, err, accounts.ErrUserVersion); err != nil {
			return err
		}
	}
	if plan.RevokePrevious {
		if err := administrationRecords(records).revokeTargetLinks(
			ctx,
			plan.Target.User.UserID,
			link.CreatedAtMS,
		); err != nil {
			return err
		}
	}
	_, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO account_links(
id,kind,invited_role,target_user_id,created_by_user_id,created_at_ms,expires_at_ms,version)
VALUES(?,?,?,?,?,?,?,1)`,
		link.AccountLinkID,
		link.Kind,
		link.Role,
		link.TargetUserID,
		link.CreatedBy.UserID,
		link.CreatedAtMS,
		link.ExpiresAtMS,
	)
	if err != nil {
		return fmt.Errorf("insert account link: %w", err)
	}
	return nil
}
