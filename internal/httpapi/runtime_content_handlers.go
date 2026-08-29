package httpapi

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/dosbundle"
	"retrom/internal/launch"
	"retrom/internal/rpgmaker/materializer"
)

func (server *Server) launchGame(writer http.ResponseWriter, request *http.Request) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	content, authorizedLaunchID, err := server.runtimeContent(request)
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动内容不可用", map[string]any{})
		return
	}
	isMultiDisc := content.Format == "RETROM_MULTIDISC_M3U_V1" && content.DiscCount >= 2
	file, err := server.blobs.OpenDigest(content.Digest)
	if err != nil {
		if isMultiDisc {
			logMultiDiscContentResponse(
				request.Context(), authorizedLaunchID, content.PlatformKey, content.CoreID,
				content.ArtifactVersion, content.DiscCount, "PLAYLIST", http.StatusServiceUnavailable, 0,
				"CAS_UNAVAILABLE",
			)
		}
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "游戏内容不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	stat, err := file.Stat()
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "游戏内容不可用", map[string]any{})
		return
	}
	mediaType := mime.TypeByExtension(filepath.Ext(request.PathValue("logicalName")))
	if content.Format == "RETROM_MULTIDISC_M3U_V1" {
		mediaType = "audio/x-mpegurl; charset=utf-8"
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	body, etag, err := launchGameBody(file, stat.Size(), content)
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "游戏内容不可用", map[string]any{})
		return
	}
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("Cache-Control", immutablePrivateContent)
	writer.Header().Set("ETag", `"sha256-`+etag+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	metricsWriter := &multiDiscResponseWriter{ResponseWriter: writer}
	http.ServeContent(metricsWriter, request, request.PathValue("logicalName"), time.Unix(0, 0), body)
	server.recordMultiDiscContentResponse(request, authorizedLaunchID, content, metricsWriter)
}

func (server *Server) launchProjectFile(writer http.ResponseWriter, request *http.Request) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	requestedIdentity := request.PathValue("contentIdentity")
	logicalName := request.PathValue("projectPath")
	grant, valid := server.runtimeProjectContentGrant(request, requestedIdentity)
	if !valid {
		writeError(
			writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID",
			"项目内容不可用", map[string]any{},
		)
		return
	}
	launchID := grant.LaunchID
	if strings.HasPrefix(logicalName, "__retrom__/packs/") {
		server.launchRuntimePackFile(writer, request, launchID, grant.Capability, logicalName)
		return
	}
	if logicalName == "index.json" {
		if index, err := server.launcher.ProjectIndex(
			request.Context(), launchID, grant.Capability,
		); err == nil {
			serveProjectIndex(writer, request, index)
			return
		}
		// EasyRPG's loader requests index.json beside the project files. The
		// generated index remains reserved internally, so uploads cannot replace it.
		logicalName = "__retrom__/index.json"
	}
	content, err := server.launcher.Content(request.Context(), launchID, grant.Capability, logicalName)
	if err != nil {
		content, err = server.launcher.ReviewPreviewProjectContent(
			request.Context(), launchID, grant.Capability, logicalName,
		)
	}
	if err != nil || content.Format != "RPG_MAKER_PROJECT_V1" && content.Format != "ONS_PROJECT_V1" &&
		content.Format != "KIRIKIRI_PROJECT_V1" {
		writeError(
			writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID",
			"项目内容不可用", map[string]any{},
		)
		return
	}
	file, err := server.blobs.OpenDigest(content.Digest)
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "项目内容不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	mediaType := mime.TypeByExtension(filepath.Ext(logicalName))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("Cache-Control", immutablePrivateContent)
	writer.Header().Set("ETag", `"sha256-`+content.Digest+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(writer, request, filepath.Base(logicalName), time.Unix(0, 0), file)
}

func (server *Server) runtimeProjectContentGrant(
	request *http.Request,
	requestedIdentity string,
) (runtimeContentGrant, bool) {
	grants, valid := runtimeContentGrants(request)
	if !valid {
		return runtimeContentGrant{}, false
	}
	for _, grant := range grants {
		identity, err := server.launcher.ProjectContentIdentity(
			request.Context(), grant.LaunchID, grant.Capability,
		)
		if err == nil && identity == requestedIdentity {
			return grant, true
		}
	}
	return runtimeContentGrant{}, false
}

func (server *Server) launchRuntimePackFile(
	writer http.ResponseWriter,
	request *http.Request,
	launchID string,
	capability string,
	logicalName string,
) {
	parts := strings.Split(logicalName, "/")
	if len(parts) < 4 || parts[0] != "__retrom__" || parts[1] != "packs" {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "项目内容不可用", map[string]any{})
		return
	}
	slot, err := strconv.Atoi(parts[2])
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "项目内容不可用", map[string]any{})
		return
	}
	if len(parts) == 4 && parts[3] == "index.json" {
		index, indexErr := server.launcher.RuntimePackIndex(request.Context(), launchID, capability, slot)
		if indexErr != nil {
			writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "项目内容不可用", map[string]any{})
			return
		}
		serveProjectIndex(writer, request, index)
		return
	}
	if len(parts) < 5 || parts[3] != "files" {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "项目内容不可用", map[string]any{})
		return
	}
	packFile, fileErr := server.launcher.RuntimePackFile(
		request.Context(), launchID, capability, slot, strings.Join(parts[4:], "/"),
	)
	if fileErr != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "项目内容不可用", map[string]any{})
		return
	}
	file, openErr := server.blobs.OpenDigest(packFile.Digest)
	if openErr != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "项目内容不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close runtime pack file", file.Close()) }()
	stat, statErr := file.Stat()
	if statErr != nil || stat.Size() != packFile.SizeBytes {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "项目内容不可用", map[string]any{})
		return
	}
	mediaType := mime.TypeByExtension(filepath.Ext(logicalName))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("Cache-Control", immutablePrivateContent)
	writer.Header().Set("ETag", `"sha256-`+packFile.Digest+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(writer, request, filepath.Base(logicalName), time.Unix(0, 0), file)
}

func serveProjectIndex(writer http.ResponseWriter, request *http.Request, index launch.ProjectIndexView) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", immutablePrivateContent)
	writer.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	writer.Header().Set("ETag", `"sha256-`+index.SHA256+`"`)
	http.ServeContent(writer, request, "index.json", time.Unix(0, 0), bytes.NewReader(index.Contents))
}

func (server *Server) recordMultiDiscContentResponse(
	request *http.Request,
	authorizedLaunchID string,
	content launch.ContentView,
	response *multiDiscResponseWriter,
) {
	if content.Format != "RETROM_MULTIDISC_M3U_V1" || content.DiscCount < 2 {
		return
	}
	status := response.status
	if status == 0 {
		status = http.StatusOK
	}
	resultCode := "OK"
	if status >= http.StatusBadRequest {
		resultCode = "HTTP_ERROR"
	}
	logMultiDiscContentResponse(
		request.Context(), authorizedLaunchID, content.PlatformKey, content.CoreID,
		content.ArtifactVersion, content.DiscCount, "PLAYLIST", status, response.bytes, resultCode,
	)
}

func (server *Server) runtimeContent(request *http.Request) (launch.ContentView, string, error) {
	logicalName := request.PathValue("logicalName")
	requestedIdentity := request.PathValue("contentIdentity")
	grants, valid := runtimeContentGrants(request)
	if !valid {
		return launch.ContentView{}, "", launch.ErrCredential
	}
	for _, grant := range grants {
		content, err := server.launcher.Content(request.Context(), grant.LaunchID, grant.Capability, logicalName)
		if err != nil {
			content, err = server.launcher.ReviewPreviewContent(
				request.Context(), grant.LaunchID, grant.Capability, logicalName,
			)
		}
		identity, identityErr := launch.ContentIdentity(content)
		if err == nil && identityErr == nil && identity == requestedIdentity {
			return content, grant.LaunchID, nil
		}
	}
	return launch.ContentView{}, "", launch.ErrCredential
}

func launchGameBody(file io.ReadSeeker, size int64, content launch.ContentView) (io.ReadSeeker, string, error) {
	if content.Format != "RETROM_DOS_DIRECT_ZIP_V1" {
		return file, content.Digest, nil
	}
	if content.CoreID != "dosbox_pure" {
		return nil, "", fmt.Errorf("launch game body: %w", dosbundle.ErrInvalid)
	}
	readerAt, ok := file.(io.ReaderAt)
	if !ok {
		return nil, "", fmt.Errorf("launch game reader: %w", dosbundle.ErrInvalid)
	}
	var overlay *dosbundle.Overlay
	var err error
	if content.DOSEntry == nil {
		overlay, err = dosbundle.NewMenu(readerAt, size)
	} else {
		overlay, err = dosbundle.New(readerAt, size, *content.DOSEntry)
	}
	if err != nil {
		return nil, "", fmt.Errorf("build DOS overlay: %w", err)
	}
	identity, err := launch.ContentIdentity(content)
	if err != nil {
		return nil, "", fmt.Errorf("derive DOS content identity: %w", err)
	}
	return overlay, identity, nil
}

func (server *Server) launchExternalFile(writer http.ResponseWriter, request *http.Request) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	content, authorizedLaunchID, err := server.runtimeExternal(request)
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动外部文件不可用", map[string]any{})
		return
	}
	file, err := server.blobs.OpenDigest(content.Digest)
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动外部文件不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Cache-Control", immutablePrivateContent)
	writer.Header().Set("ETag", `"sha256-`+content.Digest+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	metricsWriter := &multiDiscResponseWriter{ResponseWriter: writer}
	http.ServeContent(metricsWriter, request, request.PathValue("logicalName"), time.Unix(0, 0), file)
	server.recordExternalContentResponse(request, authorizedLaunchID, content, metricsWriter)
}

func (server *Server) recordExternalContentResponse(
	request *http.Request,
	authorizedLaunchID string,
	content launch.ExternalView,
	response *multiDiscResponseWriter,
) {
	if content.Kind != "DISC" {
		return
	}
	status := response.status
	if status == 0 {
		status = http.StatusOK
	}
	resultCode := "OK"
	if status >= http.StatusBadRequest {
		resultCode = "HTTP_ERROR"
	}
	logMultiDiscContentResponse(
		request.Context(), authorizedLaunchID, content.PlatformKey, content.CoreKey,
		content.ArtifactVersion, content.DiscCount, "DISC", status, response.bytes, resultCode,
	)
}

func (server *Server) runtimeExternal(request *http.Request) (launch.ExternalView, string, error) {
	logicalName := request.PathValue("logicalName")
	requestedIdentity := request.PathValue("contentIdentity")
	grants, valid := runtimeContentGrants(request)
	if !valid {
		return launch.ExternalView{}, "", launch.ErrCredential
	}
	for _, grant := range grants {
		content, err := server.launcher.External(request.Context(), grant.LaunchID, grant.Capability, logicalName)
		if err != nil {
			content, err = server.launcher.ReviewPreviewExternal(
				request.Context(), grant.LaunchID, grant.Capability, logicalName,
			)
		}
		identity, identityErr := launch.ExternalContentIdentity(content.Digest)
		if err == nil && identityErr == nil && identity == requestedIdentity {
			return content, grant.LaunchID, nil
		}
	}
	return launch.ExternalView{}, "", launch.ErrCredential
}

func (server *Server) launchBIOSBundle(writer http.ResponseWriter, request *http.Request) {
	server.launchBundle(writer, request, "BIOS_BUNDLE")
}

func (server *Server) launchParentBundle(writer http.ResponseWriter, request *http.Request) {
	server.launchBundle(writer, request, "PARENT")
}

func (server *Server) populateLaunchBundle(archiveWriter *zip.Writer, files []launch.BundleFile) (string, string) {
	if _, err := launch.BundleIdentity(files); err != nil {
		return "LAUNCH_DEPENDENCY_INVALID", "启动依赖清单无效"
	}
	ordered := slices.Clone(files)
	slices.SortFunc(ordered, func(left, right launch.BundleFile) int {
		return strings.Compare(left.LogicalName, right.LogicalName)
	})
	for _, entry := range ordered {
		destination, err := archiveWriter.CreateHeader(materializer.StoreZIPHeader(entry.LogicalName))
		if err != nil {
			return "CAS_UNAVAILABLE", "无法装配启动依赖"
		}
		source, err := server.blobs.OpenDigest(entry.SHA256)
		if err != nil {
			return "CAS_UNAVAILABLE", "启动依赖不可用"
		}
		_, copyErr := io.Copy(destination, source)
		cleanup.Error("close", source.Close())
		if copyErr != nil {
			return "CAS_UNAVAILABLE", "无法读取启动依赖"
		}
	}
	return "", ""
}

func (server *Server) launchBundle(writer http.ResponseWriter, request *http.Request, kind string) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	files, err := server.runtimeBundleFiles(request, kind)
	if errors.Is(err, launch.ErrCredential) {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	temporary, err := server.createLaunchBundle(files)
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法装配启动依赖", map[string]any{})
		return
	}
	temporaryPath := temporary.Name()
	defer cleanup.Remove(temporaryPath)
	defer func() { cleanup.Error("close", temporary.Close()) }()
	digest := sha256.New()
	if _, err := io.Copy(digest, temporary); err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法校验启动依赖", map[string]any{})
		return
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法读取启动依赖", map[string]any{})
		return
	}
	writer.Header().Set("Content-Type", "application/zip")
	writer.Header().Set("Cache-Control", immutablePrivateContent)
	writer.Header().Set("ETag", `"sha256-`+hex.EncodeToString(digest.Sum(nil))+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(writer, request, "bundle.zip", time.Unix(0, 0), temporary)
}

func (server *Server) createLaunchBundle(files []launch.BundleFile) (*os.File, error) {
	if len(files) == 0 {
		return nil, launch.ErrBlocked
	}
	temporary, err := os.CreateTemp(filepath.Join(server.config.DataDir, "tmp", "jobs"), ".launch-bundle-")
	if err != nil {
		return nil, fmt.Errorf("create launch bundle: %w", err)
	}
	if err := temporary.Chmod(0o600); err != nil {
		cleanup.Error("close", temporary.Close())
		cleanup.Remove(temporary.Name())
		return nil, fmt.Errorf("secure launch bundle: %w", err)
	}
	archiveWriter := zip.NewWriter(temporary)
	if code, _ := server.populateLaunchBundle(archiveWriter, files); code != "" {
		cleanup.Error("close", archiveWriter.Close())
		cleanup.Error("close", temporary.Close())
		cleanup.Remove(temporary.Name())
		return nil, launch.ErrBlocked
	}
	if err := archiveWriter.Close(); err != nil {
		cleanup.Error("close", temporary.Close())
		cleanup.Remove(temporary.Name())
		return nil, fmt.Errorf("close launch bundle: %w", err)
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		cleanup.Error("close", temporary.Close())
		cleanup.Remove(temporary.Name())
		return nil, fmt.Errorf("rewind launch bundle: %w", err)
	}
	return temporary, nil
}

func (server *Server) runtimeBundleFiles(request *http.Request, kind string) ([]launch.BundleFile, error) {
	requestedIdentity := request.PathValue("contentIdentity")
	grants, valid := runtimeContentGrants(request)
	if !valid {
		return nil, launch.ErrCredential
	}
	for _, grant := range grants {
		files, err := server.launcher.BundleFiles(request.Context(), grant.LaunchID, grant.Capability, kind)
		if err != nil {
			files, err = server.launcher.ReviewPreviewBundleFiles(
				request.Context(), grant.LaunchID, grant.Capability, kind,
			)
		}
		identity, identityErr := launch.BundleIdentity(files)
		if err == nil && identityErr == nil && identity == requestedIdentity {
			return files, nil
		}
	}
	return nil, launch.ErrCredential
}
