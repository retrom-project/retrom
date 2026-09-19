package hasheous

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	metadatamodel "retrom/internal/model/metadata"

	"retrom/internal/foundation/cleanup"
)

const (
	lookupURL       = "https://hasheous.org/api/v1/Lookup/ByHash"
	maximumBodySize = 4 << 20
)

var (
	errOutboundAddressRejected = errors.New("outbound address rejected")
	errOutboundDNSFailed       = errors.New("outbound DNS failed")
	errOutboundIPRejected      = errors.New("outbound IP rejected")
	errCachedMissBody          = errors.New("cached miss unexpectedly has a response body")
	errNonCacheableOutcome     = errors.New("non-cacheable provider outcome")
	errInvalidTitle            = errors.New("invalid title")
	errTrailingJSON            = errors.New("trailing JSON value")
	errInvalidProviderGameID   = errors.New("invalid provider game id")
)

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Provider struct {
	client   HTTPDoer
	resolver Resolver
	now      func() time.Time
}

func New(client HTTPDoer, resolver Resolver, now func() time.Time) *Provider {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if client == nil {
		client = &http.Client{
			Timeout:       30 * time.Second,
			Transport:     restrictedTransport(resolver),
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if now == nil {
		now = time.Now
	}
	return &Provider{client: client, resolver: resolver, now: now}
}

func restrictedTransport(resolver Resolver) *http.Transport {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		defaultTransport = &http.Transport{}
	}
	transport := defaultTransport.Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host != "hasheous.org" || port != "443" {
			return nil, errOutboundAddressRejected
		}
		addresses, err := resolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, errOutboundDNSFailed
		}
		for _, candidate := range addresses {
			if unsafeIP(candidate.IP) {
				return nil, errOutboundIPRejected
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
	}
	return transport
}

func (provider *Provider) LookupByHash(
	ctx context.Context,
	hashes metadatamodel.ContentHashes,
) (metadatamodel.LookupResult, error) {
	body, canonical, err := metadatamodel.LookupRequest(hashes)
	if err != nil {
		return metadatamodel.LookupResult{}, fmt.Errorf("prepare Hasheous lookup hashes: %w", err)
	}
	digest := sha256.Sum256(canonical)
	result := metadatamodel.LookupResult{RequestBody: body, RequestDigest: hex.EncodeToString(digest[:])}
	deadlineContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(deadlineContext, http.MethodPost, lookupURL, bytes.NewReader(body))
	if err != nil {
		return metadatamodel.LookupResult{}, fmt.Errorf("create Hasheous lookup request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := provider.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(deadlineContext.Err(), context.DeadlineExceeded) {
			result.Outcome = metadatamodel.OutcomeTimeout
			return result, nil
		}
		result.Outcome = metadatamodel.OutcomeNetworkError
		return result, nil
	}
	defer func() { cleanup.Error("close", response.Body.Close()) }()
	result.Audit = encodeHTTPAudit(response.StatusCode)
	result.RetryAfterNS = int64(parseRetryAfter(response.Header.Get("Retry-After"), provider.now()))
	contents, readErr := readBounded(response.Body, maximumBodySize)
	if readErr != nil {
		if errors.Is(readErr, errTooLarge) {
			result.Outcome = metadatamodel.OutcomeInvalidResponse
		} else {
			result.Outcome = metadatamodel.OutcomeNetworkError
		}
		return result, nil
	}
	result.RawResponse = contents
	switch response.StatusCode {
	case http.StatusNotFound:
		result.Outcome = metadatamodel.OutcomeMiss
		return result, nil
	case http.StatusTooManyRequests:
		result.Outcome = metadatamodel.OutcomeRateLimited
		return result, nil
	case http.StatusOK:
		candidate, normalizeErr := normalizeCandidate(contents, provider.now().UTC().Year())
		if normalizeErr != nil {
			result.Outcome = metadatamodel.OutcomeInvalidResponse
			return result, nil
		}
		result.Outcome, result.Candidate = metadatamodel.OutcomeHit, &candidate
		return result, nil
	default:
		if response.StatusCode >= 500 && response.StatusCode <= 599 {
			result.Outcome = metadatamodel.OutcomeNetworkError
		} else {
			result.Outcome = metadatamodel.OutcomeInvalidResponse
		}
		return result, nil
	}
}

// RestoreCached re-applies the current bounded normalizer to immutable cached
// bytes. Cache entries never bypass response validation after an upgrade.
func (provider *Provider) RestoreCached(
	hashes metadatamodel.ContentHashes,
	outcome metadatamodel.ProviderOutcome,
	audit metadatamodel.ProtocolAudit,
	raw []byte,
) (metadatamodel.LookupResult, error) {
	body, canonical, err := metadatamodel.LookupRequest(hashes)
	if err != nil {
		return metadatamodel.LookupResult{}, fmt.Errorf("prepare Hasheous lookup hashes: %w", err)
	}
	digest := sha256.Sum256(canonical)
	result := metadatamodel.LookupResult{
		Outcome:       outcome,
		Audit:         bytes.Clone(audit),
		RequestBody:   body,
		RequestDigest: hex.EncodeToString(digest[:]),
		RawResponse:   bytes.Clone(raw),
	}
	switch outcome {
	case metadatamodel.OutcomeMiss:
		if len(raw) != 0 {
			return metadatamodel.LookupResult{}, errCachedMissBody
		}
		return result, nil
	case metadatamodel.OutcomeHit:
		candidate, normalizeErr := normalizeCandidate(raw, provider.now().UTC().Year())
		if normalizeErr != nil {
			return metadatamodel.LookupResult{}, normalizeErr
		}
		result.Candidate = &candidate
		return result, nil
	case metadatamodel.OutcomeRateLimited, metadatamodel.OutcomeTimeout,
		metadatamodel.OutcomeInvalidResponse, metadatamodel.OutcomeNetworkError:
		return metadatamodel.LookupResult{}, errNonCacheableOutcome
	default:
		return metadatamodel.LookupResult{}, errNonCacheableOutcome
	}
}

type providerResponse struct {
	ID        json.Number `json:"id"`
	Name      string      `json:"name"`
	Publisher struct {
		Name string `json:"name"`
	} `json:"publisher"`
	Platform struct {
		Name string `json:"name"`
	} `json:"platform"`
	Signature struct {
		Game struct {
			Name        string      `json:"name"`
			Publisher   string      `json:"publisher"`
			Description string      `json:"description"`
			Year        string      `json:"year"`
			Score       json.Number `json:"score"`
		} `json:"game"`
		ROM struct {
			Score json.Number `json:"score"`
		} `json:"rom"`
	} `json:"signature"`
	Attributes []providerAttribute `json:"attributes"`
}

type providerAttribute struct {
	Name                  string          `json:"attributeName"`
	AttributeType         string          `json:"attributeType"`
	AttributeRelationType string          `json:"attributeRelationType"`
	Value                 json.RawMessage `json:"value"`
	Link                  string          `json:"link"`
}

func normalizeCandidate(contents []byte, normalizationYear int) (metadatamodel.Candidate, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var source providerResponse
	if err := decoder.Decode(&source); err != nil {
		return metadatamodel.Candidate{}, fmt.Errorf("hasheous/provider: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return metadatamodel.Candidate{}, err
	}
	providerID, err := positiveInteger(source.ID)
	if err != nil {
		return metadatamodel.Candidate{}, err
	}
	warnings := make([]string, 0)
	title := source.Name
	if strings.TrimSpace(title) == "" {
		title = source.Signature.Game.Name
	}
	title, valid, truncated := normalizeText(title, 200, false)
	if !valid || title == "" {
		return metadatamodel.Candidate{}, errInvalidTitle
	}
	if truncated {
		warnings = append(warnings, "FIELD_TRUNCATED:title")
	}
	description, descriptionWarnings := normalizeDescription(source)
	warnings = append(warnings, descriptionWarnings...)
	publisher := source.Publisher.Name
	if strings.TrimSpace(publisher) == "" {
		publisher = source.Signature.Game.Publisher
	}
	publisher, _, publisherTruncated := normalizeText(publisher, 200, true)
	if publisherTruncated {
		warnings = append(warnings, "FIELD_TRUNCATED:publisher")
	}
	releaseYear, yearWarning := normalizeYear(source.Signature.Game.Year, normalizationYear)
	if yearWarning {
		warnings = append(warnings, "FIELD_INVALID:releaseYear")
	}
	gameScore, gameScoreWarning := optionalScore(source.Signature.Game.Score)
	if gameScoreWarning {
		warnings = append(warnings, "FIELD_INVALID:providerGameScore")
	}
	romScore, romScoreWarning := optionalScore(source.Signature.ROM.Score)
	if romScoreWarning {
		warnings = append(warnings, "FIELD_INVALID:providerRomScore")
	}
	assets, assetWarnings := normalizeAssets(source.Attributes)
	warnings = append(warnings, assetWarnings...)
	metadata := normalizedMetadata{
		SchemaVersion: 1,
		Title:         title,
		Description:   description,
		Developer:     "",
		Publisher:     publisher,
		Genre:         "",
		Players:       nil,
		ReleaseYear:   releaseYear,
	}
	evidence := normalizedEvidence{
		SchemaVersion:     1,
		NormalizerVersion: "HASHEOUS_BY_HASH_V1",
		NormalizationYear: normalizationYear,
		PlatformName:      strings.TrimSpace(source.Platform.Name),
		ProviderGameScore: gameScore,
		ProviderRomScore:  romScore,
		Warnings:          warnings,
	}
	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return metadatamodel.Candidate{}, fmt.Errorf("encode candidate metadata: %w", err)
	}
	encodedEvidence, err := json.Marshal(evidence)
	if err != nil {
		return metadatamodel.Candidate{}, fmt.Errorf("encode candidate evidence: %w", err)
	}
	return metadatamodel.Candidate{
		ProviderGameID: providerID,
		Metadata:       encodedMetadata,
		Evidence:       encodedEvidence,
		Assets:         assets,
	}, nil
}

func normalizeDescription(source providerResponse) (string, []string) {
	descriptionSource := source.Signature.Game.Description
	warnings := make([]string, 0, 2)
	if strings.TrimSpace(descriptionSource) == "" {
		for _, attribute := range source.Attributes {
			if attribute.Name != "AIDescription" || attribute.AttributeType != "LongString" ||
				attribute.AttributeRelationType != "None" {
				continue
			}
			var fallback string
			if json.Unmarshal(attribute.Value, &fallback) == nil && strings.TrimSpace(fallback) != "" {
				descriptionSource = fallback
				warnings = append(warnings, "FIELD_FALLBACK:description:AIDescription")
				break
			}
		}
	}
	description, truncated := normalizeDescriptionText(descriptionSource, 10_000)
	if truncated {
		warnings = append(warnings, "FIELD_TRUNCATED:description")
	}
	return description, warnings
}

func normalizeDescriptionText(value string, maximum int) (string, bool) {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) {
		return "", false
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return "", false
		}
	}
	runes := []rune(value)
	if len(runes) > maximum {
		return string(runes[:maximum]), true
	}
	return value, false
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errTrailingJSON
		}
		return fmt.Errorf("hasheous/provider: %w", err)
	}
	return nil
}

func positiveInteger(number json.Number) (string, error) {
	value := number.String()
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return "", errInvalidProviderGameID
	}
	return value, nil
}

func normalizeText(value string, maximum int, optional bool) (string, bool, bool) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) {
		return "", optional, false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", optional, false
		}
	}
	runes := []rune(value)
	if len(runes) > maximum {
		return string(runes[:maximum]), true, true
	}
	return value, true, false
}

func normalizeYear(value string, normalizationYear int) (*int, bool) {
	if value == "" {
		return nil, false
	}
	if len(value) != 4 || strings.Trim(value, "0123456789") != "" {
		return nil, true
	}
	year, _ := strconv.Atoi(value)
	if year < 1950 || year > normalizationYear+1 {
		return nil, true
	}
	return &year, false
}

func optionalScore(number json.Number) (*int64, bool) {
	if number == "" {
		return nil, false
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || value < 0 || strconv.FormatInt(value, 10) != number.String() {
		return nil, true
	}
	return &value, false
}

func normalizeAssets(attributes []providerAttribute) ([]metadatamodel.AssetReference, []string) {
	assets := make([]metadatamodel.AssetReference, 0, 5)
	warnings := make([]string, 0)
	seen := make(map[string]struct{})
	for _, attribute := range attributes {
		var value string
		if attribute.AttributeType == "ImageId" {
			// Hasheous attributes are heterogeneous: for example Tags uses an
			// object value. Decode only the ImageId values Retrom consumes.
			_ = json.Unmarshal(attribute.Value, &value)
		}
		if attribute.AttributeType != "ImageId" || attribute.AttributeRelationType != "None" ||
			!validOpaqueID(value) ||
			attribute.Link != "/api/v1/images/"+value {
			continue
		}
		kind, ordinal, ok := assetSlot(attribute.Name)
		if !ok {
			continue
		}
		slot := kind + ":" + strconv.Itoa(ordinal)
		if _, duplicate := seen[slot]; duplicate {
			warnings = append(warnings, "DUPLICATE_ASSET_SLOT:"+slot)
			continue
		}
		seen[slot] = struct{}{}
		assets = append(
			assets,
			metadatamodel.AssetReference{ProviderAssetID: value, Kind: kind, Ordinal: ordinal, Path: attribute.Link},
		)
	}
	return assets, warnings
}

func assetSlot(name string) (string, int, bool) {
	if name == "Logo" {
		return "COVER", 0, true
	}
	if strings.HasPrefix(name, "Screenshot") && len(name) == len("Screenshot1") {
		value := int(name[len(name)-1] - '1')
		if value >= 0 && value <= 3 {
			return "SCREENSHOT", value, true
		}
	}
	return "", 0, false
}

func validOpaqueID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range []byte(value) {
		if character < 0x21 || character > 0x7e || character == '/' || character == '?' || character == '#' ||
			character == '%' {
			return false
		}
	}
	return true
}

var errTooLarge = errors.New("response exceeds byte limit")

func readBounded(reader io.Reader, maximum int64) ([]byte, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("hasheous/provider: %w", err)
	}
	if int64(len(contents)) > maximum {
		return nil, errTooLarge
	}
	return contents, nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return min(time.Duration(seconds)*time.Second, 15*time.Minute)
	}
	if deadline, err := http.ParseTime(value); err == nil && deadline.After(now) {
		return min(deadline.Sub(now), 15*time.Minute)
	}
	return 0
}
