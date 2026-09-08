package runtimeprovider

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"

	"retrom/internal/cleanup"
	"retrom/internal/runtimebundle"
	"retrom/internal/scummvm"
)

const scummVMAssets = "assets/scummvm/"

var ErrScummVMNotInstalled = errors.New("SCUMMVM_NOT_INSTALLED")

var scummVMEngine = regexp.MustCompile(`^[a-z0-9_]{1,128}$`)

type scummVMToolManifest struct {
	SchemaVersion  int               `json:"schemaVersion"`
	AdapterABI     string            `json:"adapterAbi"`
	UpstreamCommit string            `json:"upstreamCommit"`
	Engines        map[string]string `json:"engines"`
	Files          []json.RawMessage `json:"files"`
}

// ScummVMDetector resolves only the tool shipped by the verified active Provider.
// Its executable cache is private to the application's data directory.
func (installation Installation) ScummVMDetector(cacheRoot string) (*scummvm.Detector, error) {
	manifest, exists := installation.Manifests["retrom-runtime"]
	if !exists || !slices.ContainsFunc(manifest.Targets, func(target runtimebundle.Target) bool {
		return target.ID == "scummvm"
	}) {
		return nil, ErrScummVMNotInstalled
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return nil, ErrInstallationInvalid
	}
	tool, err := installation.scummVMTool(cacheRoot)
	if err != nil {
		return nil, err
	}
	return scummvm.New(func(context.Context) (scummvm.Tool, error) { return tool, nil }), nil
}

func (installation Installation) scummVMTool(cacheRoot string) (scummvm.Tool, error) {
	var directory string
	for _, provider := range installation.Active.Providers {
		if provider.ProviderID == "retrom-runtime" {
			directory = filepath.Join(installation.installedRoot, provider.InstallationPath)
		}
	}
	if directory == "" {
		return scummvm.Tool{}, ErrInstallationInvalid
	}
	files := make(map[string]runtimebundle.IntegrityFile)
	for _, file := range installation.Integrity["retrom-runtime"] {
		files[file.Path] = file
	}
	manifestFile, exists := files[scummVMAssets+"manifest.json"]
	if !exists || manifestFile.SizeBytes > 1024*1024 {
		return scummvm.Tool{}, ErrInstallationInvalid
	}
	manifestPath := filepath.Join(directory, manifestFile.Path)
	if err := verifyInstalledFile(manifestPath, manifestFile); err != nil {
		return scummvm.Tool{}, err
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return scummvm.Tool{}, installationInvalid(err)
	}
	manifest, err := parseScummVMToolManifest(data)
	if err != nil {
		return scummvm.Tool{}, err
	}
	engines := make([]string, 0, len(manifest.Engines))
	for engine, plugin := range manifest.Engines {
		if _, exists := files[scummVMAssets+plugin]; !exists {
			return scummvm.Tool{}, ErrInstallationInvalid
		}
		engines = append(engines, engine)
	}
	slices.Sort(engines)
	executable, exists := files[scummVMAssets+"native/linux-x86_64/scummvm-detector"]
	if !exists {
		return scummvm.Tool{}, ErrInstallationInvalid
	}
	path, err := prepareNativeTool(filepath.Join(directory, executable.Path), cacheRoot, executable)
	if err != nil {
		return scummvm.Tool{}, err
	}
	return scummvm.Tool{Path: path, UpstreamCommit: manifest.UpstreamCommit, Engines: engines}, nil
}

func parseScummVMToolManifest(contents []byte) (scummVMToolManifest, error) {
	if len(contents) > 1024*1024 {
		return scummVMToolManifest{}, ErrInstallationInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var manifest scummVMToolManifest
	if decoder.Decode(&manifest) != nil || decoder.Decode(new(any)) != io.EOF || manifest.SchemaVersion != 1 ||
		manifest.AdapterABI != "scummvm-host-v1" || len(manifest.UpstreamCommit) != 40 ||
		len(manifest.Engines) == 0 || len(manifest.Engines) > 256 {
		return scummVMToolManifest{}, ErrInstallationInvalid
	}
	commit, err := hex.DecodeString(manifest.UpstreamCommit)
	if err != nil || hex.EncodeToString(commit) != manifest.UpstreamCommit {
		return scummVMToolManifest{}, ErrInstallationInvalid
	}
	for engine, path := range manifest.Engines {
		if !scummVMEngine.MatchString(engine) || path != "plugins/lib"+engine+".so" {
			return scummVMToolManifest{}, ErrInstallationInvalid
		}
	}
	return manifest, nil
}

func prepareNativeTool(source, cacheRoot string, expected runtimebundle.IntegrityFile) (string, error) {
	if expected.SizeBytes < 1 || expected.SizeBytes > 128*1024*1024 {
		return "", ErrInstallationInvalid
	}
	if err := verifyInstalledFile(source, expected); err != nil {
		return "", err
	}
	root, err := filepath.Abs(cacheRoot)
	if err != nil {
		return "", installationInvalid(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", installationInvalid(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return "", ErrInstallationInvalid
	}
	destination := filepath.Join(root, expected.SHA256)
	if info, err := os.Lstat(destination); err == nil {
		if info.Mode().Perm() != 0o500 {
			return "", ErrInstallationInvalid
		}
		return destination, verifyInstalledFile(destination, expected)
	} else if !os.IsNotExist(err) {
		return "", installationInvalid(err)
	}
	return publishNativeTool(source, root, destination, expected)
}

func publishNativeTool(source, root, destination string, expected runtimebundle.IntegrityFile) (string, error) {
	temporary, err := os.CreateTemp(root, ".scummvm-")
	if err != nil {
		return "", installationInvalid(err)
	}
	defer func() { cleanup.Error("close ScummVM executable", temporary.Close()) }()
	defer func() {
		if err := os.Remove(temporary.Name()); err != nil && !os.IsNotExist(err) {
			cleanup.Error("remove ScummVM temporary", err)
		}
	}()
	if err := copyNativeTool(source, temporary, expected); err != nil {
		return "", err
	}
	if err := os.Rename(temporary.Name(), destination); err != nil {
		return "", installationInvalid(err)
	}
	return destination, nil
}

func copyNativeTool(source string, output *os.File, expected runtimebundle.IntegrityFile) error {
	input, err := os.Open(source)
	if err != nil {
		return installationInvalid(err)
	}
	defer func() { cleanup.Error("close ScummVM source", input.Close()) }()
	size, err := io.Copy(output, io.LimitReader(input, expected.SizeBytes+1))
	if err != nil || size != expected.SizeBytes {
		return ErrInstallationInvalid
	}
	if err := output.Sync(); err != nil {
		return installationInvalid(err)
	}
	if err := verifyInstalledFile(output.Name(), expected); err != nil {
		return err
	}
	if err := output.Chmod(0o500); err != nil {
		return fmt.Errorf("protect ScummVM executable: %w", err)
	}
	return nil
}
