package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"retrom/internal/authn"
	"retrom/internal/cursor"
	libraryservice "retrom/internal/service/libraryimport"
)

var (
	errInvalidReviewQuery  = libraryservice.ErrReviewQuery
	errInvalidReviewCursor = errors.New("invalid review cursor")
)

type reviewListSpec struct {
	filter       libraryservice.ReviewQueueFilter
	after        *libraryservice.ReviewQueuePosition
	filterDigest string
}

func validReviewQueryKeys(values url.Values) bool {
	allowed := map[string]struct{}{
		"q": {}, "tagId": {}, "importJobId": {}, "pegasusImportId": {}, "emulationStationImportId": {},
		"platformInstanceId": {}, "blockerCode": {}, "sort": {}, "cursor": {}, "limit": {},
	}
	for key := range values {
		if _, ok := allowed[key]; !ok {
			return false
		}
	}
	return true
}

func reviewListLimit(values url.Values) (int, error) {
	if values.Get("limit") == "" {
		return libraryservice.ReviewQueuePageLimit, nil
	}
	parsed, err := strconv.Atoi(values.Get("limit"))
	if err != nil || parsed < 1 || parsed > libraryservice.ReviewQueuePageLimit {
		return 0, errInvalidReviewQuery
	}
	return parsed, nil
}

func (server *Server) prepareReviewList(values url.Values, principalID string) (reviewListSpec, error) {
	if !validReviewQueryKeys(values) {
		return reviewListSpec{}, errInvalidReviewQuery
	}
	limit, err := reviewListLimit(values)
	if err != nil {
		return reviewListSpec{}, err
	}
	filter, err := libraryservice.NormalizeReviewQueueFilter(libraryservice.ReviewQueueFilter{
		Query: values.Get("q"), TagID: values.Get("tagId"), ImportJobID: values.Get("importJobId"),
		PegasusImportID: values.Get("pegasusImportId"), EmulationStationImportID: values.Get("emulationStationImportId"),
		PlatformInstanceID: values.Get("platformInstanceId"), BlockerCode: values.Get("blockerCode"),
		Sort: values.Get("sort"), Limit: limit,
	})
	if err != nil {
		return reviewListSpec{}, errInvalidReviewQuery
	}
	spec := reviewListSpec{filter: filter, filterDigest: cursor.FilterDigest(map[string]any{
		"principalId": principalID, "q": filter.Query, "tagId": filter.TagID, "importJobId": filter.ImportJobID,
		"pegasusImportId": filter.PegasusImportID, "emulationStationImportId": filter.EmulationStationImportID,
		"platformInstanceId": filter.PlatformInstanceID, "blockerCode": filter.BlockerCode,
	})}
	if err := server.applyReviewListCursor(&spec, values.Get("cursor")); err != nil {
		return reviewListSpec{}, err
	}
	return spec, nil
}

func (server *Server) applyReviewListCursor(spec *reviewListSpec, token string) error {
	if token == "" {
		return nil
	}
	payload, err := server.cursors.Decode(token, "getAdminReviews", spec.filterDigest, spec.filter.Sort)
	if err != nil || len(payload.SortValues) != 1 {
		return errInvalidReviewCursor
	}
	updatedAt, err := strconv.ParseInt(payload.SortValues[0], 10, 64)
	if err != nil || updatedAt < 0 {
		return errInvalidReviewCursor
	}
	spec.after = &libraryservice.ReviewQueuePosition{UpdatedAtMS: updatedAt, ItemID: payload.ID}
	return nil
}

func (server *Server) reviews(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	spec, err := server.prepareReviewList(request.URL.Query(), principal.UserID)
	if errors.Is(err, errInvalidReviewCursor) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
		return
	}
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "待审核筛选无效", map[string]any{})
		return
	}
	page, err := server.reviewQueue.List(request.Context(), spec.filter, spec.after)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var nextCursor *string
	if page.Next != nil {
		token, err := server.cursors.Encode(cursor.Payload{
			OperationID: "getAdminReviews", FilterDigest: spec.filterDigest, SortCode: spec.filter.Sort,
			SortValues: []string{strconv.FormatInt(page.Next.UpdatedAtMS, 10)}, ID: page.Next.ItemID,
		})
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		nextCursor = &token
	}
	writeJSON(writer, http.StatusOK, struct {
		Items      []libraryservice.ReviewQueueItem `json:"items"`
		NextCursor *string                          `json:"nextCursor"`
	}{Items: page.Items, NextCursor: nextCursor})
}
