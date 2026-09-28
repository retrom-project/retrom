package httpapi

import (
	"net/http"

	"retrom/internal/application"
	dbapi "retrom/internal/database"
	"retrom/internal/dependencies"
	"retrom/internal/launch"
)

// Database and mutable adapters belong to test fixtures, not production handlers.
type testServer struct {
	*Server
	database          dbapi.DB
	readinessDatabase dbapi.DB
	dependencies      *dependencies.Set
	launchSources     *launch.Sources
}

func testHTTPDependencies(services *application.Services, auth Authenticator, provider http.Handler) Dependencies {
	return Dependencies{
		Account: AccountDependencies{
			Authenticator: auth,
			Accounts:      services.Accounts,
		},
		Library: LibraryDependencies{
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
		Import: ImportDependencies{
			Uploads:    services.Uploads,
			Importer:   services.Importer,
			Discards:   services.ImportDiscards,
			Admissions: services.ImportAdmissions,
			Reads:      services.ImportReads,
			Server:     services.ServerImports,
			Source:     services.SourceImports,
		},
		Review: ReviewDependencies{
			Queue:         services.ReviewQueue,
			Details:       services.ReviewDetails,
			CoverUploads:  services.ReviewCoverUploads,
			Discards:      services.ReviewDiscards,
			Approvals:     services.ReviewApprovals,
			BulkApprovals: services.ReviewBulkApprovals,
			Screenshots:   services.ReviewScreenshots,
			Metadata:      services.Metadata,
			Evidence:      services.MetadataEvidence,
		},
		Play: PlayDependencies{
			Launcher:        services.Launcher,
			Variants:        services.Variants,
			Saves:           services.SaveService,
			Isolation:       services.RpgIsolation,
			RuntimeSessions: services.RuntimeSessions,
			Provider:        provider,
		},
		System: SystemDependencies{
			Readiness:   services.ReadinessService,
			Jobs:        services.JobService,
			Diagnostics: services.DiagnosticsService,
			Idempotency: services.IdempotencyService,
			Cleanup:     services.CleanupJobs,
		},
		Content: ContentDependencies{
			Files:       services.Blobs,
			Credentials: services.Credentials,
			Access:      services.MediaAccess,
		},
	}
}
