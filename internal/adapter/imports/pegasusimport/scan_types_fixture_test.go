package pegasusimport

import (
	pegasusimportmodel "retrom/internal/model/pegasusimport"
	application "retrom/internal/service/pegasusimport"
)

type (
	scanResult      = application.ScanResult
	scannedMetadata = pegasusimportmodel.ScanMetadata
	scannedItem     = pegasusimportmodel.ScanItem
)
