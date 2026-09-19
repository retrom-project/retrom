package metadatascrape

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	metadatamodel "retrom/internal/model/metadata"
)

var errResponseAuditVersion = errors.New("unsupported metadata protocol audit version")

func cachedResponseAudit(status sql.NullInt64) metadatamodel.ProtocolAudit {
	if !status.Valid || status.Int64 == 0 {
		return nil
	}
	return metadatamodel.ProtocolAudit(`{"schemaVersion":1,"httpStatus":` + strconv.FormatInt(status.Int64, 10) + `}`)
}

func responseAuditStatus(audit metadatamodel.ProtocolAudit) (sql.NullInt64, error) {
	if len(audit) == 0 {
		return sql.NullInt64{}, nil
	}
	var protocol struct {
		SchemaVersion int `json:"schemaVersion"`
		HTTPStatus    int `json:"httpStatus"`
	}
	if err := json.Unmarshal(audit, &protocol); err != nil {
		return sql.NullInt64{}, fmt.Errorf("decode metadata protocol audit: %w", err)
	}
	if protocol.SchemaVersion != 1 {
		return sql.NullInt64{}, errResponseAuditVersion
	}
	if protocol.HTTPStatus == 0 {
		return sql.NullInt64{}, nil
	}
	return sql.NullInt64{Int64: int64(protocol.HTTPStatus), Valid: true}, nil
}
