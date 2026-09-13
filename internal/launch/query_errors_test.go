package launch

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"retrom/internal/runtimelaunch"

	_ "modernc.org/sqlite" // Register the driver for cancellation regression queries.
)

func TestResourceQueriesPreserveCancellation(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	}()
	service := New(database, nil, nil, func() time.Time { return time.UnixMilli(1) })
	service.runtimeBuilder = &runtimelaunch.Builder{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for name, query := range cancelledResourceQueries(service) {
		t.Run(name, func(t *testing.T) {
			if err := query(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("query error = %v; want cancellation cause", err)
			}
		})
	}
}

func cancelledResourceQueries(service *Service) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"save": func(ctx context.Context) error { _, err := service.SaveAccess(ctx, "id", "capability"); return err },
		"content": func(ctx context.Context) error {
			_, err := service.Content(ctx, "id", "capability", "file")
			return err
		},
		"authorized": func(ctx context.Context) error {
			_, err := service.ContentAuthorized(ctx, "id", "file", false)
			return err
		},
		"rpg": func(ctx context.Context) error {
			_, err := service.RPGProjectContentAuthorized(ctx, "id", "file", false)
			return err
		},
		"external": func(ctx context.Context) error {
			_, err := service.External(ctx, "id", "capability", "file")
			return err
		},
		"external-blob": func(ctx context.Context) error {
			_, err := service.ExternalBlob(ctx, "id", "capability", "file")
			return err
		},
		"preview": func(ctx context.Context) error {
			_, err := service.ReviewPreviewContent(ctx, "id", "capability", "file")
			return err
		},
		"preview-external": func(ctx context.Context) error {
			_, err := service.ReviewPreviewExternal(ctx, "id", "capability", "file")
			return err
		},
		"preview-project": func(ctx context.Context) error {
			_, err := service.ReviewPreviewProjectContent(ctx, "id", "capability", "file")
			return err
		},
		"preview-authorized": func(ctx context.Context) error {
			_, err := service.ContentAuthorized(ctx, "id", "file", true)
			return err
		},
		"bundle": func(ctx context.Context) error {
			_, err := service.BundleFiles(ctx, "id", "capability", "BIOS_BUNDLE")
			return err
		},
		"preview-bundle": func(ctx context.Context) error {
			_, err := service.ReviewPreviewBundleFiles(ctx, "id", "capability", "PARENT")
			return err
		},
		"telemetry": func(ctx context.Context) error {
			_, err := service.MultiDiscTelemetryDimensions(ctx, "id", "capability")
			return err
		},
		"asset": func(ctx context.Context) error {
			_, err := service.ProviderAssetAuthorized(ctx, "id", false, "asset.js")
			return err
		},
	}
}
