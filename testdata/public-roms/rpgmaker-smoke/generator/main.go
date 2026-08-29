package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type mvSpec struct {
	Generation          string  `json:"generation"`
	Directory           string  `json:"directory"`
	Marker              string  `json:"marker"`
	SourceArchive       string  `json:"sourceArchive"`
	SourceArchiveSize   int64   `json:"sourceArchiveSize"`
	SourceArchiveSHA256 string  `json:"sourceArchiveSha256"`
	SourcePrefix        string  `json:"sourcePrefix"`
	AccentRGB           [3]byte `json:"accentRgb"`
}

type lcfSpec struct {
	Generation string  `json:"generation"`
	Directory  string  `json:"directory"`
	Marker     string  `json:"marker"`
	LDBID      int32   `json:"ldbId"`
	AccentRGB  [3]byte `json:"accentRgb"`
}

type rgssSpec struct {
	Generation    string  `json:"generation"`
	Directory     string  `json:"directory"`
	Marker        string  `json:"marker"`
	ProjectMarker string  `json:"projectMarker"`
	ScriptsPath   string  `json:"scriptsPath"`
	RGSSVersion   int     `json:"rgssVersion"`
	AccentRGB     [3]byte `json:"accentRgb"`
}

type securitySpec struct {
	SchemaVersion int `json:"schemaVersion"`
}

func main() {
	var output string
	var source string
	flag.StringVar(&output, "output", "", "generated fixture root")
	flag.StringVar(&source, "source", "", "rpgmaker-smoke source root")
	flag.Parse()
	if output == "" || source == "" || flag.NArg() != 0 {
		fatal(errors.New("usage: generator --source <root> --output <root>"))
	}
	if err := generate(filepath.Clean(source), filepath.Clean(output)); err != nil {
		fatal(err)
	}
}

func generate(source, output string) error {
	if source == "." || output == "." || source == output {
		return errors.New("source and output must be distinct explicit directories")
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	for _, name := range []string{"rpg2000.json", "rpg2003.json"} {
		var spec lcfSpec
		if err := decodeJSON(filepath.Join(source, "fixture-spec", name), &spec); err != nil {
			return err
		}
		if err := generateLCF(output, spec); err != nil {
			return err
		}
	}
	var rgss []rgssSpec
	if err := decodeJSON(filepath.Join(source, "fixture-spec", "rgss.json"), &rgss); err != nil {
		return err
	}
	template, err := os.ReadFile(filepath.Join(source, "fixture-spec", "rgss-smoke.rb.tmpl"))
	if err != nil {
		return fmt.Errorf("read RGSS template: %w", err)
	}
	for _, spec := range rgss {
		if err := generateRGSS(output, spec, string(template)); err != nil {
			return err
		}
	}
	var mv mvSpec
	if err := decodeJSON(filepath.Join(source, "fixture-spec", "mv.json"), &mv); err != nil {
		return err
	}
	if err := generateMV(source, output, mv); err != nil {
		return err
	}
	var security securitySpec
	if err := decodeJSON(filepath.Join(source, "fixture-spec", "security.json"), &security); err != nil {
		return err
	}
	if security.SchemaVersion != 1 {
		return errors.New("security fixture schema version must be 1")
	}
	if err := generateSecurityFixtures(output); err != nil {
		return err
	}
	return writeFixtureManifest(output)
}

func decodeJSON(path string, target any) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(contents, target); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return nil
}

func writeFile(root, relative string, contents []byte) error {
	target := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create %s parent: %w", relative, err)
	}
	if err := os.WriteFile(target, contents, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", relative, err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		return fmt.Errorf("set %s permissions: %w", relative, err)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
