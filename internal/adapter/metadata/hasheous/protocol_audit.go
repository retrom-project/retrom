package hasheous

import (
	"strconv"

	metadatamodel "retrom/internal/model/metadata"
)

func encodeHTTPAudit(status int) metadatamodel.ProtocolAudit {
	return metadatamodel.ProtocolAudit(`{"schemaVersion":1,"httpStatus":` + strconv.Itoa(status) + `}`)
}
