package httpapi

import (
	"net/http"
)

func (s *Server) routes(mux *http.ServeMux) {
	routes := []struct {
		pattern string
		handle  handler
		admin   bool
	}{
		{"POST /api/v1/admin/game-scans/inspect", s.inspectSource, true},
		{"POST /api/v1/admin/game-scans", s.createScan, true},
		{"POST /api/v1/admin/bios-scans", s.createBiosScan, true},
		{"GET /api/v1/admin/scans", s.scans, true},
		{"POST /api/v1/admin/scans/{scanId}/cancel", s.cancelScan, true},
		{"POST /api/v1/admin/games/{gameId}/runtime-options/scummvm", s.scummvmCandidates, true},
		{"GET /api/v1/admin/games/{gameId}/runtime-options/dos", s.dosEntryCandidates, true},
		{"POST /api/v1/admin/games/{gameId}/media", s.uploadMedia, true},
		{"DELETE /api/v1/admin/games/{gameId}/media/{mediaId}", s.deleteMedia, true},
		{"POST /api/v1/admin/games/{gameId}/content-replacement", s.replaceContent, true},
		{"GET /api/v1/runtime/catalog", s.catalog, false},
		{"GET /api/v1/platform-instances", s.directories, false},
		{"GET /api/v1/admin/platform-instances", s.directories, true},
		{"POST /api/v1/admin/platform-instances", s.writeDirectory, true},
		{"PATCH /api/v1/admin/platform-instances/{directoryId}", s.writeDirectory, true},
		{"DELETE /api/v1/admin/platform-instances/{directoryId}", s.deleteDirectory, true},
		{"GET /api/v1/tags", s.tags, false},
		{"GET /api/v1/admin/tags", s.tags, true},
		{"POST /api/v1/admin/tags", s.writeTag, true},
		{"PATCH /api/v1/admin/tags/{tagId}", s.writeTag, true},
		{"DELETE /api/v1/admin/tags/{tagId}", s.deleteTag, true},
		{"GET /api/v1/games", s.games, false},
		{"GET /api/v1/games/{gameId}", s.game, false},
		{"GET /api/v1/games/{gameId}/media/{mediaId}", s.media, false},
		{"GET /api/v1/admin/games", s.games, true},
		{"GET /api/v1/admin/games/{gameId}", s.game, true},
		{"PATCH /api/v1/admin/games/{gameId}", s.writeGame, true},
		{"DELETE /api/v1/admin/games/{gameId}", s.deleteGame, true},
		{"GET /api/v1/admin/reviews", s.games, true},
		{"POST /api/v1/admin/reviews/readiness", s.reviewReadiness, true},
		{"GET /api/v1/admin/reviews/{gameId}", s.game, true},
		{"PATCH /api/v1/admin/reviews/{gameId}", s.writeGame, true},
		{"POST /api/v1/admin/reviews/{gameId}/approve", s.approve, true},
		{"POST /api/v1/admin/reviews/{gameId}/discard", s.deleteGame, true},
		{"GET /api/v1/favorites", s.favorites, false},
		{"PUT /api/v1/favorites/{gameId}", s.setFavorite, false},
		{"DELETE /api/v1/favorites/{gameId}", s.removeFavorite, false},
		{"GET /api/v1/favorite-folders", s.folders, false},
		{"POST /api/v1/favorite-folders", s.writeFolder, false},
		{"PATCH /api/v1/favorite-folders/{folderId}", s.writeFolder, false},
		{"DELETE /api/v1/favorite-folders/{folderId}", s.deleteFolder, false},
		{"GET /api/v1/recent-games", s.recent, false},
		{"GET /api/v1/home", s.home, false},
		{"POST /api/v1/runs", s.createRun, false},
		{"GET /api/v1/runs/{runId}", s.getRun, false},
		{"DELETE /api/v1/runs/{runId}", s.closeRun, false},
		{"POST /api/v1/runs/{runId}/events", s.runEvent, false},
		{"GET /api/v1/runs/{runId}/resources/{resourceId}/{filename}", s.runResource, false},
		{"GET /api/v1/runs/{runId}/resources/index/{indexId}", s.runIndex, false},
		{"GET /api/v1/saves", s.saves, false},
		{"POST /api/v1/saves", s.writeSave, false},
		{"PUT /api/v1/saves/{saveId}", s.writeSave, false},
		{"PATCH /api/v1/saves/{saveId}", s.renameSave, false},
		{"DELETE /api/v1/saves/{saveId}", s.deleteSave, false},
		{"GET /api/v1/saves/{saveId}/payload", s.saveFile, false},
		{"GET /api/v1/saves/{saveId}/screenshot", s.saveFile, false},
		{"GET /api/v1/admin/bios", s.bios, true},
		{"PUT /api/v1/admin/bios/{requirementKey}", s.uploadBios, true},
		{"DELETE /api/v1/admin/bios/{requirementKey}", s.deleteBios, true},
		{"GET /api/v1/admin/source-directories", s.sourceDirectories, true},
		{"GET /api/v1/admin/account-links", s.links, true},
		{"POST /api/v1/admin/invitations", s.invitation, true},
		{"POST /api/v1/admin/users/{userId}/password-reset-links", s.resetLink, true},
		{"DELETE /api/v1/admin/account-links/{linkId}", s.revokeLink, true},
	}
	for _, route := range routes {
		mux.HandleFunc(route.pattern, s.authorized(route.handle, route.admin))
	}
	mux.HandleFunc("POST /api/v1/auth/account-links/inspect", s.open(s.inspectLink))
	mux.HandleFunc("POST /api/v1/auth/invitations/accept", s.open(s.acceptLink))
	mux.HandleFunc("POST /api/v1/auth/password-resets/complete", s.open(s.completeReset))
	mux.HandleFunc("GET /runtime/providers/{providerId}/{bundle}/{asset...}", s.authorized(s.providerAsset, false))
	mux.HandleFunc("GET /__retrom/runtime-isolation/{runId}/{asset}", s.public(s.isolationAsset))
}
