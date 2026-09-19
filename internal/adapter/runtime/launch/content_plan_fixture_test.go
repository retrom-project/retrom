package launch

import (
	launchmodel "retrom/internal/model/launch"
)

type (
	lockedDisc        = launchmodel.ProductDisc
	lockedContentFile = launchmodel.ProductContentFile
	launchContentPlan struct {
		ContentKind string
		Files       []lockedContentFile
		Discs       []lockedDisc
	}
)
