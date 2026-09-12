package launch

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"github.com/google/uuid"
)

func (service *Service) isolatedRuntimeTicket(sessionID string) (string, string, [32]byte, error) {
	var empty [32]byte
	if service.rpgRuntimeOriginTemplate == "" || strings.Count(service.rpgRuntimeOriginTemplate, "{launchId}") != 1 {
		return "", "", empty, ErrBlocked
	}
	origin := strings.Replace(service.rpgRuntimeOriginTemplate, "{launchId}", sessionID, 1)
	parsed, err := uuid.Parse(sessionID)
	if err != nil {
		return "", "", empty, ErrBlocked
	}
	capability := service.credentials.Capability(parsed)
	ticketBytes := sha256.Sum256(append([]byte("retrom-provider-bootstrap-v1\x00"), capability[:]...))
	ticket := base64.RawURLEncoding.EncodeToString(ticketBytes[:])
	hash := sha256.Sum256(ticketBytes[:])
	return origin, ticket, hash, nil
}
