package accounts

import (
	"context"
	"fmt"
	"time"
)

type LinkService struct {
	repository LinkRepository
	tokens     LinkTokenReader
	now        func() time.Time
}

func NewLinks(repository LinkRepository, tokens LinkTokenReader, now func() time.Time) *LinkService {
	return &LinkService{repository, tokens, now}
}

func (service *LinkService) Inspect(ctx context.Context, kind, token string) (LinkInspection, error) {
	if !validLinkKind(kind) {
		return LinkInspection{}, ErrAccountLinkUnavailable
	}
	id, valid := service.tokens.ParseAccountLinkToken(kind, token)
	if !valid {
		return LinkInspection{}, ErrAccountLinkUnavailable
	}
	record, found, err := service.repository.Current(ctx, id.String())
	if err != nil {
		return LinkInspection{}, fmt.Errorf("inspect account link: %w", err)
	}
	if !found || record.Link.Kind != kind || accountLinkState(record.Link, service.now().UnixMilli()) != "ACTIVE" {
		return LinkInspection{}, ErrAccountLinkUnavailable
	}
	return LinkInspection{
		Kind:        kind,
		Role:        record.Link.Role,
		Username:    record.TargetUsername,
		ExpiresAtMS: record.Link.ExpiresAtMS,
	}, nil
}

func (service *LinkService) List(ctx context.Context, filter LinkListFilter) ([]AccountLink, error) {
	filter, err := validateLinkFilter(filter)
	if err != nil {
		return nil, err
	}
	now := service.now().UnixMilli()
	records, err := service.repository.List(ctx, LinkQuery{Filter: filter, Now: now})
	if err != nil {
		return nil, fmt.Errorf("list account links: %w", err)
	}
	result := make([]AccountLink, len(records))
	for index, record := range records {
		result[index] = record.Link
		result[index].State = accountLinkState(record.Link, now)
	}
	return result, nil
}

func accountLinkState(link AccountLink, now int64) string {
	switch {
	case link.ConsumedAtMS != nil:
		return "CONSUMED"
	case link.RevokedAtMS != nil:
		return "REVOKED"
	case now >= link.ExpiresAtMS:
		return "EXPIRED"
	default:
		return "ACTIVE"
	}
}
func validLinkKind(kind string) bool { return kind == "INVITATION" || kind == "PASSWORD_RESET" }
func validateLinkFilter(filter LinkListFilter) (LinkListFilter, error) {
	if !validLinkKind(filter.Kind) {
		return filter, ErrAccountLinkUnavailable
	}
	if filter.State == "" {
		filter.State = "ACTIVE"
	}
	switch filter.State {
	case "ACTIVE", "CONSUMED", "REVOKED", "EXPIRED", "ALL":
	default:
		return filter, ErrAccountLinkUnavailable
	}
	if filter.Limit == 0 {
		filter.Limit = 51
	}
	if filter.Limit < 1 || filter.Limit > 101 || filter.AfterAtMS < 0 {
		return filter, ErrAccountLinkUnavailable
	}
	if filter.AfterID == "" && filter.AfterAtMS != 0 {
		return filter, ErrAccountLinkUnavailable
	}
	return filter, nil
}
