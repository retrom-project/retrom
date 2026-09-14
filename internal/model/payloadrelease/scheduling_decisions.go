package payloadrelease

import "math"

// ReleaseDecision is the pure outcome of evaluating one payload owner. The
// service and transaction-side repository coordinators both use it; neither
// keeps a second copy of the RETAINED/RELEASING/RELEASED/FAILED rules.
type ReleaseDecision struct {
	ExistingJobID string
	ScopeVersion  int64
	Schedule      bool
}

func DecideOwnerRelease(owner Owner, reason Reason) (ReleaseDecision, error) {
	if owner.PayloadState != "RETAINED" {
		jobID, err := ExistingOwnerRelease(owner)
		if err != nil {
			return ReleaseDecision{}, err
		}
		return ReleaseDecision{ExistingJobID: jobID}, nil
	}
	if owner.Version < 1 || owner.Version == math.MaxInt64 || !ValidReason(reason) {
		return ReleaseDecision{}, ErrScopeInvalid
	}
	return ReleaseDecision{Schedule: true, ScopeVersion: owner.Version + 1}, nil
}

func ExistingOwnerRelease(owner Owner) (string, error) {
	if owner.ReleaseJobID != "" && (owner.PayloadState == "RELEASING" || owner.PayloadState == "RELEASED" ||
		owner.PayloadState == "FAILED") {
		return owner.ReleaseJobID, nil
	}
	return "", ErrScopeInvalid
}

func SourceReleaseReason(scopeType ScopeType) (Reason, error) {
	switch scopeType {
	case ScopePegasusImportItem:
		return ReasonPegasusTerminal, nil
	case ScopeEmulationStationImportItem:
		return ReasonEmulationStationTerminal, nil
	case ScopeImportItem, ScopeImportJob, ScopeUploadConsumption, ScopeGame, ScopeBlob:
		return "", ErrScopeInvalid
	default:
		return "", ErrScopeInvalid
	}
}

type SourceLinkDecision struct {
	ExistingJobID string
}

func DecideSourceLink(source, ordinary Owner) (SourceLinkDecision, error) {
	if source.PayloadState != "RETAINED" || source.Version < 1 || source.Version == math.MaxInt64 {
		return SourceLinkDecision{}, ErrScopeInvalid
	}
	jobID, err := ExistingOwnerRelease(ordinary)
	if err != nil {
		return SourceLinkDecision{}, err
	}
	return SourceLinkDecision{ExistingJobID: jobID}, nil
}

type ConsumptionDecision struct {
	ExistingJobID string
	Schedule      bool
}

func DecideConsumption(before Consumption) ConsumptionDecision {
	if before.Released {
		return ConsumptionDecision{}
	}
	if before.ExistingJobID != "" {
		return ConsumptionDecision{ExistingJobID: before.ExistingJobID}
	}
	return ConsumptionDecision{Schedule: true}
}

func DecideGameDeletion(owner Owner, version int64) (ReleaseDecision, error) {
	if owner.Version != version || version < 1 || version == math.MaxInt64 || owner.State != "PUBLISHED" ||
		owner.PayloadState != "RETAINED" {
		return ReleaseDecision{}, ErrScopeInvalid
	}
	return ReleaseDecision{Schedule: true, ScopeVersion: version + 1}, nil
}
