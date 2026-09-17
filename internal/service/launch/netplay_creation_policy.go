package launch

import (
	model "retrom/internal/model/launch"
	"crypto/subtle"
)

func validNetplayCreationRequest(request model.NetplayCreateRequest) bool {
	return request.RoomID != "" && request.SessionID != "" && request.GameID != "" && request.GameVariantID != "" &&
		request.ProfileID != "" && request.PlayerNo >= 1 && request.PlayerNo <= 4 &&
		request.ProviderID != "" && request.TargetID != "" && validContentDigest(request.BundleSHA256) &&
		request.CredentialGeneration >= 1 && len(request.NetplayCredentialSHA256) == 32 &&
		request.ReturnTo == "/netplay/rooms/"+request.RoomID
}

func validateNetplayCreation(request model.NetplayCreateRequest, snapshot model.NetplayCreationSnapshot) error {
	if !snapshot.Found || !snapshot.Product.Found {
		return model.ErrBlocked
	}
	authority := snapshot.Authority
	if !validNetplayAuthority(request, authority) || !sameNetplaySource(snapshot.Product.Source, authority) {
		return model.ErrBlocked
	}
	if snapshot.Existing != nil {
		return validateExistingNetplay(request, authority, *snapshot.Existing)
	}
	if authority.ParticipantState != "LOCKED" || authority.Generation != 0 || request.CredentialGeneration != 1 {
		return model.ErrBlocked
	}
	return nil
}

func validNetplayAuthority(request model.NetplayCreateRequest, authority model.NetplayCreationAuthority) bool {
	if authority.SessionState == "FINISHED" || authority.SessionState == "FAILED" ||
		authority.CurrentSessionID != authority.SessionID ||
		(authority.RoomState != "STARTING" && authority.RoomState != "RUNNING") || authority.ParticipantState == "LEFT" {
		return false
	}
	return authority.SessionID == request.SessionID && authority.RoomID == request.RoomID &&
		authority.GameID == request.GameID && authority.VariantID == request.GameVariantID &&
		authority.ProviderID == request.ProviderID && authority.TargetID == request.TargetID &&
		authority.BundleDigest == request.BundleSHA256 && authority.ProfileID == request.ProfileID &&
		authority.PlayerNo == int64(request.PlayerNo)
}

func validateExistingNetplay(
	request model.NetplayCreateRequest,
	authority model.NetplayCreationAuthority,
	existing model.NetplayExistingLaunch,
) error {
	if authority.ParticipantState == "LOCKED" || authority.Generation != request.CredentialGeneration ||
		subtle.ConstantTimeCompare(authority.CredentialHash, request.NetplayCredentialSHA256) != 1 {
		return model.ErrBlocked
	}
	if existing.SessionID == nil || *existing.SessionID != request.SessionID ||
		existing.PlayerNo == nil || *existing.PlayerNo != int64(request.PlayerNo) ||
		existing.ProfileID != request.ProfileID || existing.GameID != request.GameID ||
		existing.ProviderID != request.ProviderID || existing.TargetID != request.TargetID ||
		existing.BundleDigest != request.BundleSHA256 || (existing.State != "CREATED" && existing.State != "ACTIVE") {
		return model.ErrBlocked
	}
	return nil
}

func sameNetplaySource(source model.ProductSource, authority model.NetplayCreationAuthority) bool {
	return source.GameID == authority.GameID && source.VariantID == authority.VariantID &&
		source.CoreID == authority.CoreID && source.ProviderID == authority.ProviderID &&
		source.TargetID == authority.TargetID && source.BundleSHA256 == authority.BundleDigest &&
		source.VariantStatus == "READY" && source.ContentKind == "SINGLE_FILE"
}
