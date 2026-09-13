package libraryimport

import (
	"context"

	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/libraryimport"
)

func (records reviewApprovalRecords) ClaimIdentity(ctx context.Context, platformID, digest string, now int64) error {
	result, err := records.transaction.ExecContext(ctx, `
INSERT INTO content_identity_claims(platform_id,content_identity_digest,created_at_ms)
VALUES(?,?,?) ON CONFLICT(platform_id,content_identity_digest) DO NOTHING`, platformID, digest, now)
	return approvalMutation(result, err, "claim approval identity", false)
}

func (records reviewApprovalRecords) RecordEvent(ctx context.Context, event application.ApprovalEvent) error {
	result, err := recordstore.CreateReviewEvents(ctx, records.transaction, `
INSERT INTO review_events(
 id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,after_json,diff_json,
 config_evidence_json,dat_evidence_json,provider_evidence_json,reason,created_at_ms
) VALUES(?,?,'APPROVED',?,?,?,?,?,?,?,?,?,?,?)`,
		event.ID, event.ItemID, event.ActorKind, event.ActorUserID, event.ActorLabel,
		event.BeforeJSON, event.AfterJSON, event.DiffJSON, event.ConfigJSON, event.DATJSON,
		event.ProviderJSON, event.Reason, event.NowMS)
	return approvalMutation(result, err, "insert approval event", true)
}

func (records reviewApprovalRecords) PublishItem(ctx context.Context, change application.ApprovalPublication) error {
	result, err := recordstore.UpdateImportItems(ctx, records.transaction, recordstore.Update{
		Set: `state='PUBLISHED',version=version+1,updated_at_ms=?,completed_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='REVIEW_PENDING'
AND EXISTS(SELECT 1 FROM review_drafts d WHERE d.import_item_id=import_items.id
 AND d.version=? AND d.effective_source_snapshot_id=? AND d.target_platform_instance_id=?)`,
			Args: []any{
				change.ItemID, change.ExpectedDraftVersion, change.SnapshotID,
				change.PlatformInstanceID,
			},
		},
		Values: []any{change.NowMS, change.NowMS},
	})
	if err := approvalMutation(result, err, "publish approved item", true); err != nil {
		return err
	}
	result, err = records.transaction.ExecContext(ctx, `
UPDATE import_jobs SET review_pending_item_count=review_pending_item_count-1,
 published_item_count=published_item_count+1,
 state=?,completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND review_pending_item_count=?`,
		change.Projection.State, change.Projection.CompletedAtMS, change.NowMS,
		change.ImportID, change.ExpectedParentVersion, change.ExpectedPending)
	return approvalMutation(result, err, "publish import aggregate", true)
}
