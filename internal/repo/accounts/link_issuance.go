package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/model/accounts"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

func (repository *LinkRepository) CommitIssue(
	ctx context.Context, cmd accounts.LinkIssueCommand,
) (accounts.LinkIssueResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return accounts.LinkIssueResult{},
			fmt.Errorf("begin account link issuance: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := linkRecords{accountOperations{tx}}
	replay, err := records.Replay(ctx, cmd.Operation)
	if err != nil {
		return accounts.LinkIssueResult{},
			fmt.Errorf("read account link replay: %w", err)
	}
	if err := accounts.CheckAccountReplay(replay, cmd.Operation); err != nil {
		return accounts.LinkIssueResult{}, fmt.Errorf("check account replay: %w", err)
	}
	if replay.Found {
		return commitReplayedIssue(tx, replay)
	}
	if err := validateIssueTarget(ctx, records, cmd.Plan); err != nil {
		return accounts.LinkIssueResult{}, err
	}
	if err := records.Issue(ctx, cmd.Plan); err != nil {
		return accounts.LinkIssueResult{},
			fmt.Errorf("issue account link: %w", err)
	}
	if err := records.Audit(ctx, cmd.Audit); err != nil {
		return accounts.LinkIssueResult{},
			fmt.Errorf("audit account link issuance: %w", err)
	}
	if err := records.Remember(ctx, cmd.Receipt); err != nil {
		return accounts.LinkIssueResult{},
			fmt.Errorf("remember account link: %w", err)
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return accounts.LinkIssueResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return accounts.LinkIssueResult{},
			fmt.Errorf("commit account link issuance: %w", err)
	}
	return accounts.LinkIssueResult{Link: cmd.Plan.Link, Replayed: false}, nil
}

func commitReplayedIssue(tx *sql.Tx, replay accounts.AccountReplay) (accounts.LinkIssueResult, error) {
	var link accounts.AccountLink
	if err := json.Unmarshal(replay.Body, &link); err != nil {
		return accounts.LinkIssueResult{}, fmt.Errorf("decode account link replay: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return accounts.LinkIssueResult{}, fmt.Errorf("commit account link issuance: %w", err)
	}
	return accounts.LinkIssueResult{Link: link, Replayed: true}, nil
}

func validateIssueTarget(ctx context.Context, records linkRecords, plan accounts.LinkIssuePlan) error {
	if plan.Target == nil {
		return nil
	}
	target, found, err := records.Target(ctx, plan.Target.User.UserID)
	if err != nil {
		return fmt.Errorf("read password reset target: %w", err)
	}
	if err := accounts.ValidateLinkTarget(target, found, plan.Target.Version); err != nil {
		return fmt.Errorf("validate link target: %w", err)
	}
	return nil
}

func (records linkRecords) Target(ctx context.Context, id string) (accounts.LinkTarget, bool, error) {
	var target accounts.LinkTarget
	err := records.executor.QueryRowContext(
		ctx,
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
