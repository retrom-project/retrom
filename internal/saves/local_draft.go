package saves

import (
	"context"
	"fmt"
	"net/http"
)

// CreateLocalDraft requires current account ownership, independent of expired
// runtime capabilities. The launch binding remains the authority for the target.
func (service *Service) CreateLocalDraft(ctx context.Context, launchID, userID, profileID, key string,
	request *http.Request,
) (ManualResult, bool, error) {
	launch, err := service.loadLaunch(ctx, launchID)
	if err != nil || launch.purpose != "PRODUCT" || launch.principalID != userID || launch.profileID != profileID {
		return ManualResult{}, false, ErrCredential
	}
	launch.localDraft = true
	if err := service.ensureLocalDraftWritable(ctx, service.database, launchID, launch); err != nil {
		return ManualResult{}, false, err
	}
	return service.createManualForLaunch(ctx, launchID, key, request, launch)
}

func (service *Service) ensureLocalDraftWritable(
	ctx context.Context, tx queryRower, launchID string, launch launchSnapshot,
) error {
	var allowed int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM launch_sessions session
 JOIN games game ON game.id=session.game_id
 JOIN launch_game_save_bindings binding ON binding.launch_session_id=session.id
 JOIN runtime_targets target ON target.provider_id=session.provider_id AND target.target_id=session.target_id
 WHERE session.id=? AND session.profile_id=? AND session.game_id=?
 AND session.state IN ('ACTIVE','FINISHED','EXPIRED') AND game.status='PUBLISHED'
 AND json_extract(target.checkpoint_json,'$.semantics')='GAME_SAVE'
 AND json_extract(target.checkpoint_json,'$.writeFormat')=?`,
		launchID, launch.profileID, launch.gameID, launch.checkpointFormat).Scan(&allowed)
	if err != nil {
		return fmt.Errorf("validate local draft launch: %w", err)
	}
	if allowed != 1 {
		return ErrCredential
	}
	return nil
}
