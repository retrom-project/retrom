package httpapi

import (
	"mime/multipart"
	"strconv"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"

	"retrom/internal/httpapi/generated"
	"retrom/internal/model"
)

var parentUploadSchema = sync.OnceValues(func() (*openapi3.Schema, error) {
	spec, err := generated.GetSwagger()
	if err != nil {
		return nil, wrap(err)
	}
	ref := spec.Components.Schemas["UploadGameParentMultipartBody"]
	if ref == nil || ref.Value == nil {
		return nil, model.ErrUnavailable
	}
	return ref.Value, nil
})

func parentUploadMetadata(form *multipart.Form) (int64, string, error) {
	if form == nil || len(form.Value) != 2 || len(form.Value["version"]) != 1 || len(form.Value["coreId"]) != 1 ||
		len(form.File) != 1 || len(form.File["file"]) != 1 {
		return 0, "", model.ErrInvalid
	}
	version, err := strconv.ParseInt(form.Value["version"][0], 10, 64)
	if err != nil {
		return 0, "", model.ErrInvalid
	}
	core := form.Value["coreId"][0]
	schema, err := parentUploadSchema()
	if err != nil {
		return 0, "", err
	}
	// File bytes are streamed through managed storage; scalar metadata follows the same OpenAPI authority.
	if err = schema.VisitJSON(map[string]any{"version": version, "coreId": core, "file": "uploaded"}); err != nil {
		return 0, "", model.ErrInvalid
	}
	return version, core, nil
}
