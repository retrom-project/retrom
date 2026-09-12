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

func (plan launchContentPlan) singleFile() (lockedContentFile, bool) {
	if len(plan.Files) != 1 {
		return lockedContentFile{}, false
	}
	return plan.Files[0], true
}
