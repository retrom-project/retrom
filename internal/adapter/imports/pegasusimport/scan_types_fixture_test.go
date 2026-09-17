package pegasusimport

import (
	pegasusimportmodel "retrom/internal/model/pegasusimport"
	pegasusimportservice "retrom/internal/service/pegasusimport"
)

type (
	scanResult      = pegasusimportservice.ScanResult
	scannedMetadata = pegasusimportmodel.ScanMetadata
	scannedItem     = pegasusimportmodel.ScanItem
)
