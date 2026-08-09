package hasheous

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"

	// Register the JPEG and PNG decoders used by image.DecodeConfig.
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"retrom/internal/cleanup"

	// Register the WebP decoder used by image.DecodeConfig.
	_ "golang.org/x/image/webp"
)

const (
	lookupURL       = "https://hasheous.org/api/v1/Lookup/ByHash"
	maximumBodySize = 4 << 20
	maximumAsset    = 10 << 20
	maximumPixels   = 40_000_000
)

var (
	errOutboundAddressRejected = errors.New("outbound address rejected")
	errOutboundDNSFailed       = errors.New("outbound DNS failed")
	errOutboundIPRejected      = errors.New("outbound IP rejected")
	errCachedMissBody          = errors.New("cached miss unexpectedly has a response body")
	errNonCacheableOutcome     = errors.New("non-cacheable provider outcome")
	errInvalidContentHash      = errors.New("invalid content hash")
	errMissingContentHash      = errors.New("at least one content hash is required")
	errInvalidTitle            = errors.New("invalid title")
	errTrailingJSON            = errors.New("trailing JSON value")
	errInvalidProviderGameID   = errors.New("invalid provider game id")
	ErrAssetURLInvalid         = errors.New("ASSET_URL_INVALID")
	ErrAssetNetwork            = errors.New("ASSET_NETWORK_ERROR")
	ErrAssetRedirectLimit      = errors.New("ASSET_REDIRECT_LIMIT")
	ErrAssetHTTPStatus         = errors.New("ASSET_HTTP_STATUS")
	ErrAssetTooLarge           = errors.New("ASSET_TOO_LARGE")
	ErrAssetURLRejected        = errors.New("ASSET_URL_REJECTED")
	ErrAssetDNSFailed          = errors.New("ASSET_DNS_FAILED")
	ErrAssetIPRejected         = errors.New("ASSET_IP_REJECTED")
	ErrAssetMediaTypeInvalid   = errors.New("ASSET_MEDIA_TYPE_INVALID")
	ErrAssetMediaTypeMismatch  = errors.New("ASSET_MEDIA_TYPE_MISMATCH")
	ErrAssetDecodeFailed       = errors.New("ASSET_DECODE_FAILED")
	ErrAssetPixelLimit         = errors.New("ASSET_PIXEL_LIMIT")
)

type ContentHashes struct {
	MD5    string
	SHA1   string
	SHA256 string
	CRC32  string
}

type ProviderOutcome string

const (
	OutcomeHit             ProviderOutcome = "HIT"
	OutcomeMiss            ProviderOutcome = "MISS"
	OutcomeRateLimited     ProviderOutcome = "RATE_LIMITED"
	OutcomeTimeout         ProviderOutcome = "TIMEOUT"
	OutcomeInvalidResponse ProviderOutcome = "INVALID_RESPONSE"
	OutcomeNetworkError    ProviderOutcome = "NETWORK_ERROR"
)

type Candidate struct {
	ProviderGameID   string         `json:"providerGameId"`
	Metadata         map[string]any `json:"metadata"`
	Evidence         map[string]any `json:"evidence"`
	Assets           []AssetRef     `json:"assets"`
	RawResponse      []byte         `json:"-"`
	NormalizationUTC int            `json:"-"`
}

type AssetRef struct {
	ProviderAssetID string `json:"providerAssetId"`
	Kind            string `json:"kind"`
	Ordinal         int    `json:"ordinal"`
	Path            string `json:"path"`
}

type LookupResult struct {
	Outcome       ProviderOutcome
	HTTPStatus    int
	Candidate     *Candidate
	RawResponse   []byte
	RetryAfter    time.Duration
	RequestBody   []byte
	RequestDigest string
}

type AssetData struct {
	Bytes     []byte
	MediaType string
	Width     int
	Height    int
}

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

func (provider *Provider) LookupByHash(ctx context.Context, hashes ContentHashes) (LookupResult, error) {
	body, canonical, err := lookupRequest(hashes)
	if err != nil {
		return LookupResult{}, err
	}
	digest := sha256.Sum256(canonical)
	result := LookupResult{RequestBody: body, RequestDigest: hex.EncodeToString(digest[:])}
	deadlineContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(deadlineContext, http.MethodPost, lookupURL, bytes.NewReader(body))
	if err != nil {
		return LookupResult{}, fmt.Errorf("create Hasheous lookup request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := provider.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(deadlineContext.Err(), context.DeadlineExceeded) {
			result.Outcome = OutcomeTimeout
			return result, nil
		}
		result.Outcome = OutcomeNetworkError
		return result, nil
	}
	defer func() { cleanup.Error("close", response.Body.Close()) }()
	result.HTTPStatus = response.StatusCode
	result.RetryAfter = parseRetryAfter(response.Header.Get("Retry-After"), provider.now())
	contents, readErr := readBounded(response.Body, maximumBodySize)
	if readErr != nil {
		if errors.Is(readErr, errTooLarge) {
			result.Outcome = OutcomeInvalidResponse
		} else {
			result.Outcome = OutcomeNetworkError
		}
		return result, nil
	}
	result.RawResponse = contents
	switch response.StatusCode {
	case http.StatusNotFound:
		result.Outcome = OutcomeMiss
		return result, nil
	case http.StatusTooManyRequests:
		result.Outcome = OutcomeRateLimited
		return result, nil
	case http.StatusOK:
		candidate, normalizeErr := normalizeCandidate(contents, provider.now().UTC().Year())
		if normalizeErr != nil {
			result.Outcome = OutcomeInvalidResponse
			return result, nil
		}
		candidate.RawResponse = bytes.Clone(contents)
		result.Outcome, result.Candidate = OutcomeHit, &candidate
		return result, nil
	default:
		if response.StatusCode >= 500 && response.StatusCode <= 599 {
			result.Outcome = OutcomeNetworkError
		} else {
			result.Outcome = OutcomeInvalidResponse
		}
		return result, nil
	}
}

func RequestDigest(hashes ContentHashes) (string, error) {
	_, canonical, err := lookupRequest(hashes)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

// RestoreCached re-applies the current bounded normalizer to immutable cached
// bytes. Cache entries never bypass response validation after an upgrade.
func (provider *Provider) RestoreCached(
	hashes ContentHashes,
	outcome ProviderOutcome,
	httpStatus int,
	raw []byte,
) (LookupResult, error) {
	body, canonical, err := lookupRequest(hashes)
	if err != nil {
		return LookupResult{}, err
	}
	digest := sha256.Sum256(canonical)
	result := LookupResult{
		Outcome:       outcome,
		HTTPStatus:    httpStatus,
		RequestBody:   body,
		RequestDigest: hex.EncodeToString(digest[:]),
		RawResponse:   bytes.Clone(raw),
	}
	switch outcome {
	case OutcomeMiss:
		if len(raw) != 0 {
			return LookupResult{}, errCachedMissBody
		}
		return result, nil
	case OutcomeHit:
		candidate, normalizeErr := normalizeCandidate(raw, provider.now().UTC().Year())
		if normalizeErr != nil {
			return LookupResult{}, normalizeErr
		}
		candidate.RawResponse = bytes.Clone(raw)
		result.Candidate = &candidate
		return result, nil
	case OutcomeRateLimited, OutcomeTimeout, OutcomeInvalidResponse, OutcomeNetworkError:
		return LookupResult{}, errNonCacheableOutcome
	default:
		return LookupResult{}, errNonCacheableOutcome
	}
}

func lookupRequest(hashes ContentHashes) ([]byte, []byte, error) {
	fields := []struct {
		key    string
		value  string
		length int
	}{{"crc", hashes.CRC32, 8}, {"mD5", hashes.MD5, 32}, {"shA1", hashes.SHA1, 40}, {"shA256", hashes.SHA256, 64}}
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.value == "" {
			continue
		}
		if field.value != strings.ToLower(field.value) || len(field.value) != field.length {
			return nil, nil, errInvalidContentHash
		}
		if _, err := hex.DecodeString(field.value); err != nil {
			return nil, nil, errInvalidContentHash
		}
		parts = append(parts, strconv.Quote(field.key)+":"+strconv.Quote(field.value))
	}
	if len(parts) == 0 {
		return nil, nil, errMissingContentHash
	}
	body := []byte("{" + strings.Join(parts, ",") + "}")
	canonical := []byte(`{"body":` + string(body) + `,"endpointContract":"BY_HASH_V1","provider":"HASHEOUS"}`)
	return body, canonical, nil
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

func normalizeCandidate(contents []byte, normalizationYear int) (Candidate, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var source providerResponse
	if err := decoder.Decode(&source); err != nil {
		return Candidate{}, fmt.Errorf("hasheous/provider: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Candidate{}, err
	}
	providerID, err := positiveInteger(source.ID)
	if err != nil {
		return Candidate{}, err
	}
	warnings := make([]string, 0)
	title := source.Name
	if strings.TrimSpace(title) == "" {
		title = source.Signature.Game.Name
	}
	title, valid, truncated := normalizeText(title, 200, false)
	if !valid || title == "" {
		return Candidate{}, errInvalidTitle
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
	metadata := map[string]any{
		"schemaVersion": 1,
		"title":         title,
		"description":   description,
		"developer":     "",
		"publisher":     publisher,
		"genre":         "",
		"players":       nil,
		"releaseYear":   releaseYear,
	}
	evidence := map[string]any{
		"schemaVersion":     1,
		"normalizerVersion": "HASHEOUS_BY_HASH_V1",
		"normalizationYear": normalizationYear,
		"platformName":      strings.TrimSpace(source.Platform.Name),
		"providerGameScore": gameScore,
		"providerRomScore":  romScore,
		"warnings":          warnings,
	}
	return Candidate{
		ProviderGameID:   providerID,
		Metadata:         metadata,
		Evidence:         evidence,
		Assets:           assets,
		NormalizationUTC: normalizationYear,
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
	var trailing any
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

func normalizeYear(value string, normalizationYear int) (any, bool) {
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
	return year, false
}

func optionalScore(number json.Number) (any, bool) {
	if number == "" {
		return nil, false
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || value < 0 || strconv.FormatInt(value, 10) != number.String() {
		return nil, true
	}
	return value, false
}

func normalizeAssets(attributes []providerAttribute) ([]AssetRef, []string) {
	assets := make([]AssetRef, 0, 5)
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
			AssetRef{ProviderAssetID: value, Kind: kind, Ordinal: ordinal, Path: attribute.Link},
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

func (provider *Provider) FetchAsset(ctx context.Context, asset AssetRef) (AssetData, error) {
	if !validOpaqueID(asset.ProviderAssetID) || asset.Path != "/api/v1/images/"+asset.ProviderAssetID {
		return AssetData{}, ErrAssetURLInvalid
	}
	current := &url.URL{Scheme: "https", Host: "hasheous.org", Path: asset.Path}
	deadlineContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for redirect := 0; redirect <= 3; redirect++ {
		if err := provider.validateAssetURL(deadlineContext, current); err != nil {
			return AssetData{}, err
		}
		request, err := http.NewRequestWithContext(deadlineContext, http.MethodGet, current.String(), nil)
		if err != nil {
			return AssetData{}, ErrAssetURLInvalid
		}
		response, err := provider.client.Do(request)
		if err != nil {
			return AssetData{}, ErrAssetNetwork
		}
		if response.StatusCode >= 300 && response.StatusCode <= 399 {
			location := response.Header.Get("Location")
			cleanup.Error("close", response.Body.Close())
			if redirect == 3 {
				return AssetData{}, ErrAssetRedirectLimit
			}
			next, resolveErr := current.Parse(location)
			if resolveErr != nil {
				return AssetData{}, ErrAssetURLInvalid
			}
			current = next
			continue
		}
		if response.StatusCode != http.StatusOK {
			cleanup.Error("close", response.Body.Close())
			return AssetData{}, ErrAssetHTTPStatus
		}
		contents, readErr := readBounded(response.Body, maximumAsset)
		cleanup.Error("close", response.Body.Close())
		if readErr != nil {
			if errors.Is(readErr, errTooLarge) {
				return AssetData{}, ErrAssetTooLarge
			}
			return AssetData{}, ErrAssetNetwork
		}
		return validateImage(contents, response.Header.Get("Content-Type"))
	}
	return AssetData{}, ErrAssetRedirectLimit
}

func (provider *Provider) validateAssetURL(ctx context.Context, target *url.URL) error {
	if target.Scheme != "https" || target.Hostname() != "hasheous.org" ||
		(target.Port() != "" && target.Port() != "443") ||
		target.RawQuery != "" ||
		target.Fragment != "" {
		return ErrAssetURLRejected
	}
	addresses, err := provider.resolver.LookupIPAddr(ctx, target.Hostname())
	if err != nil || len(addresses) == 0 {
		return ErrAssetDNSFailed
	}
	for _, address := range addresses {
		if unsafeIP(address.IP) {
			return ErrAssetIPRejected
		}
	}
	return nil
}

func unsafeIP(ip net.IP) bool {
	return ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

func validateImage(contents []byte, headerType string) (AssetData, error) {
	detected := http.DetectContentType(contents)
	if detected != "image/png" && detected != "image/jpeg" && detected != "image/webp" {
		return AssetData{}, ErrAssetMediaTypeInvalid
	}
	if headerType != "" {
		mediaType := strings.TrimSpace(strings.Split(headerType, ";")[0])
		if mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/webp" {
			return AssetData{}, ErrAssetMediaTypeMismatch
		}
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(contents))
	if err != nil || (format != "png" && format != "jpeg" && format != "webp") {
		return AssetData{}, ErrAssetDecodeFailed
	}
	if configuration.Width <= 0 || configuration.Height <= 0 ||
		int64(configuration.Width)*int64(configuration.Height) > maximumPixels {
		return AssetData{}, ErrAssetPixelLimit
	}
	return AssetData{
		Bytes:     contents,
		MediaType: detected,
		Width:     configuration.Width,
		Height:    configuration.Height,
	}, nil
}

func ValidateImage(contents []byte, declaredMediaType string) (AssetData, error) {
	if len(contents) > maximumAsset {
		return AssetData{}, ErrAssetTooLarge
	}
	return validateImage(contents, declaredMediaType)
}
