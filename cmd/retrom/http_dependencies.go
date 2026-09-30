package main

import (
	"net/http"

	"retrom/internal/application"
	"retrom/internal/httpapi"
)

func httpDependencies(
	services *application.Services, auth httpapi.Authenticator, provider http.Handler,
) httpapi.Dependencies {
	return httpapi.Dependencies{
		Account: httpapi.AccountDependencies{
			Authenticator: auth,
			Accounts:      services.Accounts,
		},
		Library: httpapi.LibraryDependencies{
			Catalog:     services.CatalogService,
			Directories: services.PlatformDirectories,
			Firmware:    services.Firmware,
			BIOS:        services.BiosService,
			Content:     services.GameContent,
			Impact:      services.GameImpact,
			List:        services.GameListService,
			Home:        services.HomeService,
			Assets:      services.GameAssets,
			Metadata:    services.GameMetadata,
			Moves:       services.GameMove,
			Favorites:   services.FavoriteService,
			Tags:        services.TagService,
			Immersive:   services.Immersive,
		},
		Import: httpapi.ImportDependencies{
			Uploads:    services.Uploads,
			Importer:   services.Importer,
			Discards:   services.ImportDiscards,
			Admissions: services.ImportAdmissions,
			Reads:      services.ImportReads,
			Server:     services.ServerImports,
			Source:     services.SourceImports,
		},
		Review: httpapi.ReviewDependencies{
			Queue:         services.ReviewQueue,
			Details:       services.ReviewDetails,
			CoverUploads:  services.ReviewCoverUploads,
			Discards:      services.ReviewDiscards,
			Approvals:     services.ReviewApprovals,
			BulkApprovals: services.ReviewBulkApprovals,
			Screenshots:   services.ReviewScreenshots,
			Previews:      services.ReviewPreviews,
			Metadata:      services.Metadata,
			Evidence:      services.MetadataEvidence,
		},
		Play: httpapi.PlayDependencies{
			Launcher:        services.Launcher,
			Variants:        services.Variants,
			Saves:           services.SaveService,
			Isolation:       services.RpgIsolation,
			RuntimeSessions: services.RuntimeSessions,
			Provider:        provider,
		},
		System: httpapi.SystemDependencies{
			Readiness:   services.ReadinessService,
			Jobs:        services.JobService,
			Diagnostics: services.DiagnosticsService,
			Idempotency: services.IdempotencyService,
			Cleanup:     services.CleanupJobs,
		},
		Content: httpapi.ContentDependencies{
			Files:       services.Blobs,
			Credentials: services.Credentials,
			Access:      services.MediaAccess,
		},
	}
}
