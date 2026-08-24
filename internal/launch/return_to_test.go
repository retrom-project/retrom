package launch

import "testing"

func TestValidReturnToAcceptsOnlyExactImmersiveGameList(t *testing.T) {
	t.Parallel()
	const gameID = "01980000-0000-7000-8000-000000000001"
	for _, value := range []string{
		"/immersive/platforms/gba?gameId=" + gameID,
		"/immersive/platforms/arcade?gameId=" + gameID,
	} {
		if !validReturnTo(value, gameID) {
			t.Fatalf("expected immersive return URL to be valid: %s", value)
		}
	}
	for _, value := range []string{
		"/immersive/platforms/gba",
		"/immersive/platforms/gba?gameId=other",
		"/immersive/platforms/GBA?gameId=" + gameID,
		"/immersive/platforms/gba/extra?gameId=" + gameID,
		"/immersive/platforms/gba?gameId=" + gameID + "&extra=true",
		"/immersive/platforms/gba?gameId=" + gameID + "#fragment",
		"https://example.invalid/immersive/platforms/gba?gameId=" + gameID,
	} {
		if validReturnTo(value, gameID) {
			t.Fatalf("expected immersive return URL to be rejected: %s", value)
		}
	}
}
