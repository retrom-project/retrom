UPDATE save_tab
SET status = 'deleted', version = version + 1,
    updated_at_ms = (EXTRACT(EPOCH FROM statement_timestamp()) * 1000)::BIGINT
WHERE status = 'active' AND extinfo->>'coreId' = 'rpgmaker';
