package gamecontent

import (
	"context"

	"retrom/internal/model/corevalidation"
)

// BIOSFactsReader reads installation facts from the replacement transaction.
type BIOSFactsReader interface {
	BIOS(context.Context, string, string) ([]corevalidation.BIOSRecord, error)
}
