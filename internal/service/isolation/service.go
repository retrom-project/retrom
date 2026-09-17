package isolation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const originMarker = "00000000-0000-4000-8000-000000000000"

type Service struct {
	repository Repository
	now        func() time.Time
	template   string
}

func New(repository Repository, template string, now func() time.Time) *Service {
	return &Service{repository: repository, template: template, now: now}
}

func (service *Service) ResolveHost(host string) (Access, bool) {
	parsed, suffix, ok := service.runtimeTemplate()
	if !ok || !strings.HasSuffix(host, suffix) {
		return Access{}, false
	}
	launchID := strings.TrimSuffix(host, suffix)
	parsedID, err := uuid.Parse(launchID)
	if err != nil || parsedID.String() != launchID || launchID+suffix != host {
		return Access{}, false
	}
	origin := parsed.Scheme + "://" + host
	return Access{LaunchID: launchID, Origin: origin}, true
}

func (service *Service) IsRuntimeHostCandidate(host string) bool {
	_, suffix, ok := service.runtimeTemplate()
	return ok && strings.HasSuffix(host, suffix)
}

func (service *Service) runtimeTemplate() (*url.URL, string, bool) {
	concrete := strings.Replace(service.template, "{launchId}", originMarker, 1)
	parsed, err := url.Parse(concrete)
	if err != nil || parsed.Host == "" || !strings.HasPrefix(parsed.Host, originMarker) {
		return nil, "", false
	}
	suffix := strings.TrimPrefix(parsed.Host, originMarker)
	return parsed, suffix, suffix != ""
}

func activeSession(session RuntimeSession, now int64) bool {
	return ActiveSession(session, now)
}

func (service *Service) InspectBootstrap(ctx context.Context, launchID, origin string) (Access, error) {
	ticket, err := service.repository.Bootstrap(ctx, TicketQuery{LaunchID: launchID, Origin: origin})
	if err != nil {
		return Access{}, fmt.Errorf("inspect isolated bootstrap: %w", err)
	}
	now := service.now().UnixMilli()
	if ticket.Consumed || ticket.ExpiresAtMS <= now || !activeSession(ticket.Session, now) {
		return Access{}, ErrCredential
	}
	return sessionAccess(ticket.Session, launchID, origin, ticket.ExpiresAtMS), nil
}

func credentialDigest(encoded string) ([32]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return [32]byte{}, ErrCredential
	}
	return sha256.Sum256(raw), nil
}

func (service *Service) ConsumeTicket(ctx context.Context, launchID, origin, ticket string) (string, Access, error) {
	digest, err := credentialDigest(ticket)
	if err != nil {
		return "", Access{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", Access{}, fmt.Errorf("generate isolated credential: %w", err)
	}
	credential := base64.RawURLEncoding.EncodeToString(raw)
	issuedDigest := sha256.Sum256(raw)
	now := service.now().UnixMilli()
	result, err := service.repository.ConsumeAndIssue(ctx, ConsumeAndIssueCommand{
		Query:    TicketQuery{LaunchID: launchID, Origin: origin, Digest: &digest},
		LaunchID: launchID, Origin: origin,
		Digest: issuedDigest, NowMS: now,
	})
	if err != nil {
		return "", Access{}, fmt.Errorf("isolated bootstrap: %w", err)
	}
	return credential, result.Access, nil
}

func (service *Service) Authenticate(ctx context.Context, launchID, origin, credential string) (Access, error) {
	digest, err := credentialDigest(credential)
	if err != nil {
		return Access{}, err
	}
	capability, err := service.repository.Capability(
		ctx,
		CredentialQuery{
			LaunchID: launchID,
			Origin:   origin,
			Digest:   digest,
		},
	)
	if err != nil {
		return Access{}, fmt.Errorf("authenticate isolated capability: %w", err)
	}
	now := service.now().UnixMilli()
	if capability.Revoked || capability.ExpiresAtMS <= now || !activeSession(capability.Session, now) {
		return Access{}, ErrCredential
	}
	return sessionAccess(capability.Session, launchID, origin, capability.ExpiresAtMS), nil
}

func (service *Service) Revoke(ctx context.Context, access Access) error {
	if err := service.repository.Revoke(ctx, access, service.now().UnixMilli()); err != nil {
		return fmt.Errorf("revoke isolated capability: %w", err)
	}
	return nil
}

func sessionAccess(session RuntimeSession, launchID, origin string, expires int64) Access {
	return Access{
		LaunchID: launchID, Origin: origin, Profile: session.Profile, ContentFormat: session.ContentFormat,
		Preview: session.Preview, Expires: expires,
	}
}
