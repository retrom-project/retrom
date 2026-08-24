package launch

import "testing"

func TestValidReturnToAcceptsOnlyExactImmersiveGameList(t *testing.T) {
	t.Parallel()
	const gameID = "01980000-0000-7000-8000-000000000001"
	const folderID = "01980000-0000-7000-8000-000000000002"
	const saveStateID = "01980000-0000-7000-8000-000000000003"
	for _, value := range []string{
		"/immersive/platforms/gba?gameId=" + gameID,
		"/immersive/platforms/arcade?gameId=" + gameID,
		"/immersive/library/all?gameId=" + gameID,
		"/immersive/library/recent?gameId=" + gameID,
		"/immersive/library/favorites?gameId=" + gameID,
		"/immersive/library/favorites?gameId=" + gameID + "&folderId=" + folderID,
	} {
		if !validReturnTo(value, gameID, nil) {
			t.Fatalf("expected immersive return URL to be valid: %s", value)
		}
	}
	if !validReturnTo(
		"/immersive/library/saves?gameId="+gameID+"&saveStateId="+saveStateID,
		gameID,
		stringPointer(saveStateID),
	) {
		t.Fatal("expected immersive save return URL to be valid")
	}
	for _, value := range []string{
		"/immersive/platforms/gba",
		"/immersive/platforms/gba?gameId=other",
		"/immersive/platforms/GBA?gameId=" + gameID,
		"/immersive/platforms/gba/extra?gameId=" + gameID,
		"/immersive/platforms/gba?gameId=" + gameID + "&extra=true",
		"/immersive/platforms/gba?gameId=" + gameID + "#fragment",
		"https://example.invalid/immersive/platforms/gba?gameId=" + gameID,
		"/immersive/library/all",
		"/immersive/library/recent?folderId=" + folderID,
		"/immersive/library/favorites?gameId=" + gameID + "&folderId=other",
		"/immersive/library/favorites?gameId=" + gameID + "&extra=true",
		"/immersive/library/saves?gameId=" + gameID + "&saveStateId=" + saveStateID,
	} {
		if validReturnTo(value, gameID, nil) {
			t.Fatalf("expected immersive return URL to be rejected: %s", value)
		}
	}
	if validReturnTo(
		"/immersive/platforms/gba?gameId="+gameID,
		gameID,
		stringPointer(saveStateID),
	) {
		t.Fatal("expected save restore platform return URL to be rejected")
	}
}

func stringPointer(value string) *string { return &value }
