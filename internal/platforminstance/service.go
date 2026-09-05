package platforminstance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"retrom/internal/contentprofile"
	"retrom/internal/platformcatalog"
)

const applyOperationID = "postAdminPlatformInstanceRecommendationsApply"

var (
	ErrCatalogInvalid     = errors.New("PLATFORM_CATALOG_INVALID")
	ErrDefaultCoreInvalid = errors.New("PLATFORM_DEFAULT_CORE_INVALID")
	ErrIdempotencyReused  = errors.New("IDEMPOTENCY_KEY_REUSED")
	ErrInvalid            = errors.New("INVALID_REQUEST")
	ErrSlugExhausted      = errors.New("platform slug space exhausted")
)

type State string

const (
	StateActive              State = "ACTIVE"
	StateCustomized          State = "CUSTOMIZED"
	StateCoveredByEquivalent State = "COVERED_BY_EQUIVALENT"
	StateSuppressed          State = "SUPPRESSED"
	StateMissing             State = "MISSING"
)

type Reference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Recommendation struct {
	TemplateKey         string    `json:"templateKey"`
	CatalogOrder        int       `json:"catalogOrder"`
	Name                string    `json:"name"`
	Description         string    `json:"description"`
	Platform            Reference `json:"platform"`
	DefaultCore         Reference `json:"defaultCore"`
	SupportedExtensions []string  `json:"supportedExtensions"`
	State               State     `json:"state"`
	PlatformInstanceID  *string   `json:"platformInstanceId"`
}

type RecommendationSummary struct {
	TotalCount               int `json:"totalCount"`
	ActiveCount              int `json:"activeCount"`
	CustomizedCount          int `json:"customizedCount"`
	CoveredByEquivalentCount int `json:"coveredByEquivalentCount"`
	SuppressedCount          int `json:"suppressedCount"`
	MissingCount             int `json:"missingCount"`
}

type Recommendations struct {
	CatalogVersion int                   `json:"catalogVersion"`
	Summary        RecommendationSummary `json:"summary"`
	Items          []Recommendation      `json:"items"`
}

type Instance struct {
	ID                  string   `json:"id"`
	PlatformID          string   `json:"platformId"`
	PlatformName        string   `json:"platformName"`
	DefaultCoreID       string   `json:"defaultCoreId"`
	DefaultCoreName     string   `json:"defaultCoreName"`
	Name                string   `json:"name"`
	Slug                string   `json:"slug"`
	Description         string   `json:"description"`
	SortOrder           int64    `json:"sortOrder"`
	Enabled             bool     `json:"enabled"`
	GameCount           int64    `json:"gameCount"`
	SupportedExtensions []string `json:"supportedExtensions"`
	Version             int64    `json:"version"`
	CreatedAtMS         int64    `json:"createdAtMs"`
	UpdatedAtMS         int64    `json:"updatedAtMs"`
}

type ApplySummary struct {
	CreatedCount          int `json:"createdCount"`
	CoveredCount          int `json:"coveredCount"`
	SuppressedCount       int `json:"suppressedCount"`
	RemainingMissingCount int `json:"remainingMissingCount"`
}

type ApplyResult struct {
	CatalogVersion      int              `json:"catalogVersion"`
	CreatedTemplateKeys []string         `json:"createdTemplateKeys"`
	Created             []Instance       `json:"created"`
	Summary             ApplySummary     `json:"summary"`
	Items               []Recommendation `json:"items"`
}

type IdempotentResponse struct {
	Status   int
	Headers  map[string]string
	Body     []byte
	Replayed bool
}

type AuditActor struct {
	Kind      string
	UserID    any
	Label     any
	RequestID string
}

type CreateInput struct {
	PlatformID    string
	DefaultCoreID string
	Name          string
	Description   string
	SortOrder     int64
}

type Service struct {
	database *sql.DB
	now      func() time.Time
}

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type directoryRow struct {
	ID          string
	PlatformID  string
	CoreID      string
	Name        string
	Description string
	SortOrder   int64
	Enabled     bool
	CatalogKey  sql.NullString
	DeletedAtMS sql.NullInt64
}

type catalogReference struct {
	PlatformName string
	CoreName     string
}

func New(database *sql.DB, now func() time.Time) *Service {
	return &Service{database: database, now: now}
}

func (service *Service) ValidateCatalog(ctx context.Context) error {
	catalog := platformcatalog.Current()
	if err := platformcatalog.Validate(catalog); err != nil {
		return fmt.Errorf("%w: %w", ErrCatalogInvalid, err)
	}
	_, err := service.catalogReferences(ctx, service.database, catalog)
	return err
}

func (service *Service) Recommendations(ctx context.Context) (Recommendations, error) {
	catalog := platformcatalog.Current()
	if err := platformcatalog.Validate(catalog); err != nil {
		return Recommendations{}, fmt.Errorf("%w: %w", ErrCatalogInvalid, err)
	}
	transaction, err := service.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Recommendations{}, fmt.Errorf("platforminstance: begin read: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	references, err := service.catalogReferences(ctx, transaction, catalog)
	if err != nil {
		return Recommendations{}, err
	}
	rows, err := readDirectoryRows(ctx, transaction)
	if err != nil {
		return Recommendations{}, err
	}
	result := projectRecommendations(catalog, references, rows)
	if err := transaction.Commit(); err != nil {
		return Recommendations{}, fmt.Errorf("platforminstance: commit read: %w", err)
	}
	return result, nil
}

func (service *Service) Create(ctx context.Context, actor AuditActor, input CreateInput) (Instance, error) {
	if !validText(input.Name, 1, 200, false) || !validText(input.Description, 0, 10_000, true) {
		return Instance{}, ErrInvalid
	}
	var created Instance
	err := service.withImmediateWrite(ctx, func(connection *sql.Conn) error {
		var err error
		created, err = service.createInstance(
			ctx, connection, actor, input, "", "PLATFORM_INSTANCE_CREATED", service.now().UnixMilli(),
		)
		return err
	})
	return created, err
}

func (service *Service) Apply(
	ctx context.Context,
	actor AuditActor,
	principalID string,
	key string,
) (IdempotentResponse, error) {
	if principalID == "" || key == "" {
		return IdempotentResponse{}, ErrInvalid
	}
	digestBytes := sha256.Sum256([]byte(applyOperationID + "\x00" + principalID + "\x00{}"))
	digest := hex.EncodeToString(digestBytes[:])
	var response IdempotentResponse
	err := service.withImmediateWrite(ctx, func(connection *sql.Conn) error {
		now := service.now().UnixMilli()
		if _, err := connection.ExecContext(ctx, `
DELETE FROM idempotency_records
WHERE principal_id=? AND operation_id=? AND key=? AND expires_at_ms<=?
`, principalID, applyOperationID, key, now); err != nil {
			return fmt.Errorf("platforminstance: prune idempotency: %w", err)
		}
		var storedDigest, headersJSON string
		var storedStatus int
		var storedBody []byte
		err := connection.QueryRowContext(ctx, `
SELECT request_digest,http_status,response_headers_json,response_body
FROM idempotency_records
WHERE principal_id=? AND operation_id=? AND key=?
`, principalID, applyOperationID, key).Scan(&storedDigest, &storedStatus, &headersJSON, &storedBody)
		if err == nil {
			if storedDigest != digest {
				return ErrIdempotencyReused
			}
			headers := make(map[string]string)
			if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
				return fmt.Errorf("platforminstance: decode idempotency headers: %w", err)
			}
			response = IdempotentResponse{Status: storedStatus, Headers: headers, Body: storedBody, Replayed: true}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("platforminstance: read idempotency: %w", err)
		}
		result, err := service.apply(ctx, connection, actor, now)
		if err != nil {
			return err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("platforminstance: encode result: %w", err)
		}
		body = append(body, '\n')
		headers := map[string]string{"Content-Type": "application/json; charset=utf-8"}
		headersJSONBytes, _ := json.Marshal(headers)
		if _, err := connection.ExecContext(ctx, `
INSERT INTO idempotency_records(
  principal_id,operation_id,key,request_digest,http_status,response_headers_json,response_body,
  created_at_ms,expires_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)
`, principalID, applyOperationID, key, digest, 200, string(headersJSONBytes), body, now,
			now+int64(24*time.Hour/time.Millisecond)); err != nil {
			return fmt.Errorf("platforminstance: store idempotency: %w", err)
		}
		response = IdempotentResponse{Status: 200, Headers: headers, Body: body}
		return nil
	})
	return response, err
}

func (service *Service) apply(
	ctx context.Context,
	connection *sql.Conn,
	actor AuditActor,
	now int64,
) (ApplyResult, error) {
	catalog := platformcatalog.Current()
	if err := platformcatalog.Validate(catalog); err != nil {
		return ApplyResult{}, fmt.Errorf("%w: %w", ErrCatalogInvalid, err)
	}
	references, err := service.catalogReferences(ctx, connection, catalog)
	if err != nil {
		return ApplyResult{}, err
	}
	rows, err := readDirectoryRows(ctx, connection)
	if err != nil {
		return ApplyResult{}, err
	}
	before := projectRecommendations(catalog, references, rows)
	coveredBefore := before.Summary.ActiveCount + before.Summary.CustomizedCount + before.Summary.CoveredByEquivalentCount
	maxSortOrder := int64(0)
	for _, row := range rows {
		if !row.DeletedAtMS.Valid && row.SortOrder > maxSortOrder {
			maxSortOrder = row.SortOrder
		}
	}
	nextSortOrder := int64(100)
	if maxSortOrder > 0 {
		nextSortOrder = (maxSortOrder/100 + 1) * 100
	}
	created := make([]Instance, 0, before.Summary.MissingCount)
	createdKeys := make([]string, 0, before.Summary.MissingCount)
	for _, recommendation := range before.Items {
		if recommendation.State != StateMissing {
			continue
		}
		template := catalogTemplate(catalog, recommendation.TemplateKey)
		instance, err := service.createInstance(ctx, connection, actor, CreateInput{
			PlatformID: template.PlatformID, DefaultCoreID: template.DefaultCoreID,
			Name: template.Name, Description: template.Description, SortOrder: nextSortOrder,
		}, template.Key, "PLATFORM_INSTANCE_RECOMMENDED_CREATED", now)
		if err != nil {
			return ApplyResult{}, err
		}
		created = append(created, instance)
		createdKeys = append(createdKeys, template.Key)
		nextSortOrder += 100
	}
	rows, err = readDirectoryRows(ctx, connection)
	if err != nil {
		return ApplyResult{}, err
	}
	after := projectRecommendations(catalog, references, rows)
	return ApplyResult{
		CatalogVersion: catalog.Version, CreatedTemplateKeys: createdKeys, Created: created,
		Summary: ApplySummary{
			CreatedCount: len(created), CoveredCount: coveredBefore,
			SuppressedCount: before.Summary.SuppressedCount, RemainingMissingCount: after.Summary.MissingCount,
		},
		Items: after.Items,
	}, nil
}

func (service *Service) createInstance(
	ctx context.Context,
	database executor,
	actor AuditActor,
	input CreateInput,
	catalogKey string,
	action string,
	now int64,
) (Instance, error) {
	var relationCount int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM platform_cores
WHERE platform_id=? AND core_id=? AND enabled=1
`, input.PlatformID, input.DefaultCoreID).Scan(&relationCount); err != nil {
		return Instance{}, fmt.Errorf("platforminstance: validate default core: %w", err)
	}
	if relationCount != 1 {
		return Instance{}, ErrDefaultCoreInvalid
	}
	slug, err := NextSlug(ctx, database, input.PlatformID, input.Name)
	if err != nil {
		return Instance{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Instance{}, fmt.Errorf("platforminstance: create id: %w", err)
	}
	var storedCatalogKey any
	if catalogKey != "" {
		storedCatalogKey = catalogKey
	}
	_, err = database.ExecContext(ctx, `
INSERT INTO platform_instances(
  id,platform_id,default_core_id,name,slug,description,sort_order,enabled,version,
  created_at_ms,updated_at_ms,catalog_template_key
) VALUES(?,?,?,?,?,?,?,1,1,?,?,?)
`, id.String(), input.PlatformID, input.DefaultCoreID, input.Name, slug, input.Description,
		input.SortOrder, now, now, storedCatalogKey)
	if err != nil {
		return Instance{}, fmt.Errorf("platforminstance: insert: %w", err)
	}
	if err := insertAudit(ctx, database, actor, action, id.String(), nil, map[string]any{
		"platformId": input.PlatformID, "defaultCoreId": input.DefaultCoreID, "name": input.Name,
		"slug": slug, "description": input.Description, "sortOrder": input.SortOrder,
		"catalogTemplateKey": nullableCatalogKey(catalogKey),
	}, now); err != nil {
		return Instance{}, err
	}
	return readInstance(ctx, database, id.String())
}

func (service *Service) catalogReferences(
	ctx context.Context,
	database executor,
	catalog platformcatalog.Catalog,
) (map[string]catalogReference, error) {
	result := make(map[string]catalogReference, len(catalog.Templates))
	for _, template := range catalog.Templates {
		var reference catalogReference
		err := database.QueryRowContext(ctx, `
SELECT p.name,c.name
FROM platforms p
JOIN platform_cores pc ON pc.platform_id=p.id AND pc.core_id=? AND pc.enabled=1
JOIN cores c ON c.id=pc.core_id AND c.enabled=1
WHERE p.id=? AND p.enabled=1
AND (
  SELECT count(*)
  FROM runtime_target_bindings binding
  JOIN runtime_binding_platforms binding_platform
    ON binding_platform.binding_id=binding.binding_id
   AND binding_platform.platform_id=p.id AND binding_platform.core_id=c.id
  WHERE binding.core_id=c.id AND binding.launch_policy<>'DISABLED'
) = CASE WHEN c.id='rpgmaker' THEN 7 ELSE 1 END
`, template.DefaultCoreID, template.PlatformID).
			Scan(&reference.PlatformName, &reference.CoreName)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: unresolved template %s", ErrCatalogInvalid, template.Key)
		}
		if err != nil {
			return nil, fmt.Errorf("platforminstance: validate template %s: %w", template.Key, err)
		}
		result[template.Key] = reference
	}
	return result, nil
}

func readDirectoryRows(ctx context.Context, database executor) ([]directoryRow, error) {
	rows, err := database.QueryContext(ctx, `
SELECT id,platform_id,default_core_id,name,description,sort_order,enabled,catalog_template_key,deleted_at_ms
FROM platform_instances
ORDER BY sort_order,id
`)
	if err != nil {
		return nil, fmt.Errorf("platforminstance: query directories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]directoryRow, 0)
	for rows.Next() {
		var row directoryRow
		var enabled int
		if err := rows.Scan(&row.ID, &row.PlatformID, &row.CoreID, &row.Name, &row.Description,
			&row.SortOrder, &enabled, &row.CatalogKey, &row.DeletedAtMS); err != nil {
			return nil, fmt.Errorf("platforminstance: scan directory: %w", err)
		}
		row.Enabled = enabled == 1
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("platforminstance: iterate directories: %w", err)
	}
	return result, nil
}

type directoryIndex struct {
	activeByKey     map[string]directoryRow
	activeByPair    map[string]directoryRow
	suppressedByKey map[string]directoryRow
}

func indexDirectoryRows(rows []directoryRow) directoryIndex {
	index := directoryIndex{
		activeByKey:     make(map[string]directoryRow),
		activeByPair:    make(map[string]directoryRow),
		suppressedByKey: make(map[string]directoryRow),
	}
	for _, row := range rows {
		if row.DeletedAtMS.Valid || !row.Enabled {
			addCatalogRow(index.suppressedByKey, row)
			continue
		}
		addCatalogRow(index.activeByKey, row)
		addDirectoryRow(index.activeByPair, row.PlatformID+"/"+row.CoreID, row)
	}
	return index
}

func addCatalogRow(target map[string]directoryRow, row directoryRow) {
	if row.CatalogKey.Valid {
		addDirectoryRow(target, row.CatalogKey.String, row)
	}
}

func addDirectoryRow(target map[string]directoryRow, key string, row directoryRow) {
	if _, exists := target[key]; !exists {
		target[key] = row
	}
}

func projectRecommendations(
	catalog platformcatalog.Catalog,
	references map[string]catalogReference,
	rows []directoryRow,
) Recommendations {
	index := indexDirectoryRows(rows)
	result := Recommendations{
		CatalogVersion: catalog.Version,
		Items:          make([]Recommendation, 0, len(catalog.Templates)),
	}
	result.Summary.TotalCount = len(catalog.Templates)
	for _, template := range catalog.Templates {
		reference := references[template.Key]
		item := Recommendation{
			TemplateKey: template.Key, CatalogOrder: template.CatalogOrder,
			Name: template.Name, Description: template.Description,
			Platform:            Reference{ID: template.PlatformID, Name: reference.PlatformName},
			DefaultCore:         Reference{ID: template.DefaultCoreID, Name: reference.CoreName},
			SupportedExtensions: contentprofile.SupportedExtensions(template.PlatformID),
		}
		projectRecommendationState(&item, &result.Summary, template, index)
		result.Items = append(result.Items, item)
	}
	return result
}

func projectRecommendationState(
	item *Recommendation,
	summary *RecommendationSummary,
	template platformcatalog.DirectoryTemplate,
	index directoryIndex,
) {
	if row, exists := index.activeByKey[template.Key]; exists {
		item.PlatformInstanceID = stringPointer(row.ID)
		if directoryMatchesTemplate(row, template) {
			item.State = StateActive
			summary.ActiveCount++
			return
		}
		item.State = StateCustomized
		summary.CustomizedCount++
		return
	}
	if row, exists := index.activeByPair[template.Key]; exists {
		item.State = StateCoveredByEquivalent
		item.PlatformInstanceID = stringPointer(row.ID)
		summary.CoveredByEquivalentCount++
		return
	}
	if row, exists := index.suppressedByKey[template.Key]; exists {
		item.State = StateSuppressed
		item.PlatformInstanceID = stringPointer(row.ID)
		summary.SuppressedCount++
		return
	}
	item.State = StateMissing
	summary.MissingCount++
}

func directoryMatchesTemplate(row directoryRow, template platformcatalog.DirectoryTemplate) bool {
	return row.PlatformID == template.PlatformID && row.CoreID == template.DefaultCoreID &&
		row.Name == template.Name && row.Description == template.Description
}

func readInstance(ctx context.Context, database executor, id string) (Instance, error) {
	var instance Instance
	var enabled int
	err := database.QueryRowContext(ctx, `
SELECT pi.id,pi.platform_id,p.name,pi.default_core_id,c.name,pi.name,pi.slug,pi.description,
pi.sort_order,pi.enabled,pi.version,pi.created_at_ms,pi.updated_at_ms,
(SELECT count(*) FROM games g WHERE g.platform_instance_id=pi.id)
FROM platform_instances pi
JOIN platforms p ON p.id=pi.platform_id
JOIN cores c ON c.id=pi.default_core_id
WHERE pi.id=? AND pi.deleted_at_ms IS NULL
`, id).Scan(
		&instance.ID, &instance.PlatformID, &instance.PlatformName, &instance.DefaultCoreID,
		&instance.DefaultCoreName, &instance.Name, &instance.Slug, &instance.Description,
		&instance.SortOrder, &enabled, &instance.Version, &instance.CreatedAtMS, &instance.UpdatedAtMS,
		&instance.GameCount,
	)
	if err != nil {
		return Instance{}, fmt.Errorf("platforminstance: read created directory: %w", err)
	}
	instance.Enabled = enabled == 1
	instance.SupportedExtensions = contentprofile.SupportedExtensions(instance.PlatformID)
	return instance, nil
}

func insertAudit(
	ctx context.Context,
	database executor,
	actor AuditActor,
	action, resourceID string,
	before, after any,
	now int64,
) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("platforminstance: create audit id: %w", err)
	}
	var beforeJSON, afterJSON any
	if before != nil {
		value, _ := json.Marshal(before)
		beforeJSON = string(value)
	}
	if after != nil {
		value, _ := json.Marshal(after)
		afterJSON = string(value)
	}
	_, err = database.ExecContext(ctx, `
INSERT INTO audit_events(
  id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
  before_json,after_json,diff_json,request_id,created_at_ms
) VALUES(?,?,?,?,?,'PLATFORM_INSTANCE',?,?,?,'{}',?,?)
`, id.String(), actor.Kind, actor.UserID, actor.Label, action, resourceID, beforeJSON, afterJSON, actor.RequestID, now)
	if err != nil {
		return fmt.Errorf("platforminstance: insert audit: %w", err)
	}
	return nil
}

func (service *Service) withImmediateWrite(ctx context.Context, work func(*sql.Conn) error) error {
	connection, err := service.database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("platforminstance: acquire connection: %w", err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("platforminstance: begin immediate: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()
	if err := work(connection); err != nil {
		return err
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("platforminstance: commit: %w", err)
	}
	committed = true
	return nil
}

func SlugBase(name, platformID string) string {
	toSlug := func(value string) string {
		var builder strings.Builder
		separator := false
		for _, character := range value {
			if character >= 'A' && character <= 'Z' {
				character += 'a' - 'A'
			}
			if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
				if separator && builder.Len() > 0 && builder.Len() < 80 {
					builder.WriteByte('-')
				}
				separator = false
				if builder.Len() < 80 {
					builder.WriteRune(character)
				}
				continue
			}
			separator = builder.Len() > 0
		}
		return strings.TrimRight(builder.String(), "-")
	}
	if slug := toSlug(name); slug != "" {
		return slug
	}
	prefix := toSlug(platformID)
	if prefix == "" {
		prefix = "game"
	}
	return prefix + "-library"
}

func SlugWithSuffix(base string, suffix int) string {
	if suffix < 2 {
		return base
	}
	ending := "-" + strconv.Itoa(suffix)
	prefix := strings.TrimRight(base[:min(len(base), 80-len(ending))], "-")
	return prefix + ending
}

func NextSlug(ctx context.Context, database executor, platformID, name string) (string, error) {
	base := SlugBase(name, platformID)
	prefix := base + "-"
	rows, err := database.QueryContext(ctx, `
SELECT slug FROM platform_instances
WHERE platform_id=? AND (slug=? OR substr(slug,1,?)=?)
`, platformID, base, len(prefix), prefix)
	if err != nil {
		return "", fmt.Errorf("platforminstance: query slugs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	used := make(map[string]struct{})
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return "", fmt.Errorf("platforminstance: scan slug: %w", err)
		}
		used[slug] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("platforminstance: iterate slugs: %w", err)
	}
	for suffix := 1; suffix <= len(used)+1; suffix++ {
		candidate := SlugWithSuffix(base, suffix)
		if _, exists := used[candidate]; !exists {
			return candidate, nil
		}
	}
	return "", ErrSlugExhausted
}

func validText(value string, minimum, maximum int, allowNewline bool) bool {
	if !utf8.ValidString(value) || value != strings.TrimSpace(value) {
		return false
	}
	count := 0
	for _, character := range value {
		if unicode.IsControl(character) &&
			(!allowNewline || character != '\n' && character != '\r' && character != '\t') {
			return false
		}
		count++
	}
	return count >= minimum && count <= maximum
}

func catalogTemplate(catalog platformcatalog.Catalog, key string) platformcatalog.DirectoryTemplate {
	for _, template := range catalog.Templates {
		if template.Key == key {
			return template
		}
	}
	return platformcatalog.DirectoryTemplate{}
}

func nullableCatalogKey(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func stringPointer(value string) *string {
	return &value
}
