package persistence

import (
	"context"
	"encoding/json"

	"retrom/internal/model"
)

type RuntimeFacts struct {
	GameID    string           `json:"gameId"`
	Directory model.Directory  `json:"directory"`
	Config    json.RawMessage  `json:"config"`
	Files     []model.GameFile `json:"files"`
}

func (r *Repository) RuntimeFacts(ctx context.Context, ids []string) ([]RuntimeFacts, error) {
	return listJSON[RuntimeFacts](ctx, r, `SELECT jsonb_build_object('gameId',g.id,'config',g.runtime_config_json,
 'directory',jsonb_build_object('platformId',d.platform_id,'defaultCoreId',d.default_core_id,'coreIds',
 COALESCE((SELECT jsonb_agg(pc.core_id ORDER BY pc.sort_order) FROM platform_instance_core_tab pc
 WHERE pc.platform_instance_id=d.id),'[]'::jsonb)),
 'files',COALESCE((SELECT jsonb_agg(jsonb_build_object('logicalKey',f.logical_key,'sha256',f.sha256,
 'sizeBytes',
f.size_bytes) ORDER BY f.logical_key) FROM game_file_tab f WHERE f.game_id=g.id AND f.status='active'),
'[]'::jsonb))
 FROM game_tab g JOIN platform_instance_tab d ON d.id=g.platform_instance_id
 WHERE g.id=ANY($1::text[]) AND g.status='published' AND d.enabled`, ids)
}
