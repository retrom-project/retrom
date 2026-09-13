package launch

import application "retrom/internal/service/launch"

type (
	lockedDisc        = application.ProductDisc
	lockedContentFile = application.ProductContentFile
	launchContentPlan struct {
		ContentKind string
		Files       []lockedContentFile
		Discs       []lockedDisc
	}
)
