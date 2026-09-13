package launch

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/adapter/files/blobstore"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	"retrom/internal/capability/runtime/runtimebundle"
	"retrom/internal/capability/runtime/runtimelaunch"
	application "retrom/internal/service/launch"
)

// Sources supplies host IO and credentials to the application. Configure it before serving requests.
type Sources struct {
	blobs          *blobstore.Store
	credentials    *retromruntime.Credentials
	builder        *runtimelaunch.Builder
	originTemplate string
}

func NewSources(blobs *blobstore.Store, credentials *retromruntime.Credentials) *Sources {
	return &Sources{blobs: blobs, credentials: credentials}
}

func (source *Sources) WithRuntimeProvider(builder *runtimelaunch.Builder) *Sources {
	source.builder = builder
	return source
}

func (source *Sources) WithRPGRuntimeOriginTemplate(template string) *Sources {
	source.originTemplate = template
	return source
}

func (source *Sources) Target(provider, target string) (runtimebundle.Target, bool) {
	return source.builder.Target(provider, target)
}

func (source *Sources) BundleSHA256(provider, target string) (string, bool) {
	return source.builder.BundleSHA256(provider, target)
}

func (source *Sources) Build(input runtimelaunch.Input) ([]byte, error) {
	contents, err := source.builder.Build(input)
	if err != nil {
		return nil, fmt.Errorf("build launch envelope: %w", err)
	}
	return contents, nil
}

func (source *Sources) AssetPaths(provider, target string) ([]string, bool) {
	declaration, found := source.Target(provider, target)
	return declaration.AssetPaths, found
}

func (source *Sources) Verify(ctx context.Context, check application.ProductBlobCheck) error {
	return (productBlobVerifier{blobs: source.blobs}).Verify(ctx, check)
}

func (source *Sources) Read(ctx context.Context, reader io.Reader) (application.ScreenshotImage, error) {
	return (screenshotImages{blobs: source.blobs}).Read(ctx, reader)
}

func (source *Sources) SignCapability(id string) (string, []byte, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return "", nil, fmt.Errorf("parse launch identity: %w", err)
	}
	if source.credentials == nil {
		return "", nil, application.ErrBlocked
	}
	capability := source.credentials.Capability(parsed)
	hash := retromruntime.HashCapability(capability)
	return retromruntime.EncodeCapability(capability), hash[:], nil
}

func (source *Sources) SignIsolation(id string) (application.IsolationTicket, error) {
	if source.originTemplate == "" || strings.Count(source.originTemplate, "{launchId}") != 1 ||
		source.credentials == nil {
		return application.IsolationTicket{}, application.ErrBlocked
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return application.IsolationTicket{}, application.ErrBlocked
	}
	capability := source.credentials.Capability(parsed)
	ticket := sha256.Sum256(append([]byte("retrom-provider-bootstrap-v1\x00"), capability[:]...))
	return application.IsolationTicket{
		Origin: strings.Replace(source.originTemplate, "{launchId}", id, 1),
		Ticket: base64.RawURLEncoding.EncodeToString(ticket[:]), Hash: sha256.Sum256(ticket[:]),
	}, nil
}
