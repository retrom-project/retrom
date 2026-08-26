package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const maximumMVSourceEntryBytes = 16 * 1024 * 1024

var mvCoreSHA256 = map[string]string{
	"rpg_core":     "cce476804212b28049fef8b5c117577d0e356eac4abe33804628931e1826dd41",
	"rpg_managers": "aef81821786b12a3c27f8db510089663f6c2aabb1b1708dd065f765f3d1f9c18",
	"rpg_objects":  "d4278c2161193d9105787ee6b1ff5167c07ed23af4deef190b7705a326f1926e",
	"rpg_scenes":   "b10b50dcb62793e42fd807fa3034fb82a89c6cb7ab32afafbb366d6d0a7f248a",
	"rpg_sprites":  "3b434c9b81c4081d00dd9eca3900fe7564545c6b5a70e3b00cd263de4170a4c8",
	"rpg_windows":  "81343a9b3fb1c957f1c6098776f8ca263aac8431f3996b647caab36d6a03c5f3",
}

var mvLibrarySHA256 = map[string]string{
	"fpsmeter.js":                    "fec43a13a522dafe9c28c3d30635a275af350edf3423de0349fb6fb9c01e9450",
	"iphone-inline-video.browser.js": "688ce9e9460d08399b898519b6d6811f8bd6722369e266b1f2761002be608f72",
	"lz-string.js":                   "7acc5ae524455fb67dee09375b4246386241f7dc4708dcdf8af0e78ca8267de7",
	"pixi.js":                        "47097d24b261679366419f9e36196a3303c35fa3d06d0518edb7f1ab5417def0",
	"pixi-picture.js":                "f0e2af6190f2c53361047379ff0ae041568097f1b5beadcad28012f0aa5a99bb",
	"pixi-tilemap.js":                "7401aeac40f9af7f7e777ce7a03a99c39571fa744fdb97add34732d7f8984e06",
}

func generateMV(source, output string, spec mvSpec) error {
	if spec.Generation != "RPGMV" || spec.Directory != "rpgmv" || spec.Marker != "RETROM RPGMV" ||
		spec.SourceArchive == "" || spec.SourcePrefix == "" || !strings.HasSuffix(spec.SourcePrefix, "/") {
		return errors.New("invalid RPG Maker MV fixture spec")
	}
	archivePath := filepath.Join(source, filepath.FromSlash(spec.SourceArchive))
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("read MV source archive: %w", err)
	}
	if int64(len(archiveBytes)) != spec.SourceArchiveSize || digest(archiveBytes) != spec.SourceArchiveSHA256 {
		return errors.New("RPG Maker MV source archive identity drifted")
	}
	entries, err := readMVSourceArchive(archiveBytes, spec.SourcePrefix)
	if err != nil {
		return err
	}
	root := spec.Directory
	for name, expectedSHA256 := range mvCoreSHA256 {
		var ordered []string
		if err := json.Unmarshal(entries[name+".json"], &ordered); err != nil || len(ordered) == 0 {
			return fmt.Errorf("decode locked %s.json: %w", name, err)
		}
		parts := make([][]byte, 0, len(ordered))
		seen := make(map[string]bool, len(ordered))
		for _, sourcePath := range ordered {
			if seen[sourcePath] || path.Clean(sourcePath) != sourcePath || !strings.HasPrefix(sourcePath, "js/"+name+"/") {
				return fmt.Errorf("invalid locked %s source path %q", name, sourcePath)
			}
			contents, exists := entries[sourcePath]
			if !exists {
				return fmt.Errorf("locked %s source missing: %s", name, sourcePath)
			}
			seen[sourcePath] = true
			parts = append(parts, contents)
		}
		concatenated := bytes.Join(parts, []byte{'\n'})
		if digest(concatenated) != expectedSHA256 {
			return fmt.Errorf("locked %s.js identity drifted", name)
		}
		if err := writeFile(output, root+"/js/"+name+".js", concatenated); err != nil {
			return err
		}
	}
	for name, expectedSHA256 := range mvLibrarySHA256 {
		contents, exists := entries["js/libs/"+name]
		if !exists || digest(contents) != expectedSHA256 {
			return fmt.Errorf("locked MV library identity drifted: %s", name)
		}
		if err := writeFile(output, root+"/js/libs/"+name, contents); err != nil {
			return err
		}
	}
	if err := verifyMVLicense(source, entries); err != nil {
		return err
	}
	return writeMVProject(source, output, spec)
}

func readMVSourceArchive(contents []byte, prefix string) (map[string][]byte, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("open MV source gzip: %w", err)
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	entries := make(map[string][]byte)
	for {
		header, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read MV source tar: %w", readErr)
		}
		if header.Typeflag == tar.TypeDir || header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > maximumMVSourceEntryBytes ||
			!strings.HasPrefix(header.Name, prefix) {
			return nil, fmt.Errorf("unsafe MV source archive entry: %q", header.Name)
		}
		relative := strings.TrimPrefix(header.Name, prefix)
		if relative == "" || path.Clean(relative) != relative || strings.HasPrefix(relative, "/") ||
			strings.HasPrefix(relative, "../") {
			return nil, fmt.Errorf("unsafe MV source archive path: %q", header.Name)
		}
		if _, duplicate := entries[relative]; duplicate {
			return nil, fmt.Errorf("duplicate MV source archive entry: %q", relative)
		}
		entry, readErr := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if readErr != nil || int64(len(entry)) != header.Size {
			return nil, fmt.Errorf("read MV source archive entry %q: %w", relative, readErr)
		}
		entries[relative] = entry
	}
	return entries, nil
}

func verifyMVLicense(source string, entries map[string][]byte) error {
	archiveLicense, exists := entries["LICENSE"]
	if !exists || len(archiveLicense) != 1089 || digest(archiveLicense) != "e3563d4b3ff63e95a154f75d207444d71c108dc1bb64f0bb8230c5ee65a13922" {
		return errors.New("locked MV source license identity drifted")
	}
	materialized, err := os.ReadFile(filepath.Join(source, "LICENSES", "CoreScript-MIT.txt"))
	if err != nil {
		return fmt.Errorf("read materialized MV license: %w", err)
	}
	if !bytes.Equal(materialized, archiveLicense) {
		return errors.New("materialized MV license differs from locked archive")
	}
	return nil
}

func writeMVProject(source, output string, spec mvSpec) error {
	inputs := map[string]string{
		"index.html":                "mv-index.html",
		"css/retrom.css":            "mv.css",
		"js/main.js":                "mv-main.js",
		"js/plugins/RetromSmoke.js": "mv-smoke.js.tmpl",
	}
	for target, input := range inputs {
		contents, err := os.ReadFile(filepath.Join(source, "fixture-spec", input))
		if err != nil {
			return fmt.Errorf("read MV fixture input %s: %w", input, err)
		}
		contents = bytes.ReplaceAll(contents, []byte("{{MARKER}}"), []byte(spec.Marker))
		if bytes.Contains(contents, []byte("{{")) {
			return fmt.Errorf("MV fixture input contains unresolved placeholder: %s", input)
		}
		if err := writeFile(output, spec.Directory+"/"+target, contents); err != nil {
			return err
		}
	}
	plugins := []byte("var $plugins = [{\n    \"name\": \"RetromSmoke\",\n    \"status\": true,\n    \"description\": \"Retrom-owned deterministic RPG Maker MV smoke scene.\",\n    \"parameters\": {}\n}];\n")
	if err := writeFile(output, spec.Directory+"/js/plugins.js", plugins); err != nil {
		return err
	}
	image, err := markerPNG(spec.Marker, spec.AccentRGB)
	if err != nil {
		return err
	}
	if err := writeFile(output, spec.Directory+"/img/pictures/retrom-marker.png", image); err != nil {
		return err
	}
	loading := pngRGBA(1, 1, func(_, _ int) [4]byte { return [4]byte{} })
	if err := writeFile(output, spec.Directory+"/img/system/Loading.png", loading); err != nil {
		return err
	}
	if err := writeFile(output, spec.Directory+"/audio/se/retrom-tone.wav", toneWAV()); err != nil {
		return err
	}
	return writeMVData(output, spec)
}

func writeMVData(output string, spec mvSpec) error {
	emptyDatabase := []any{nil}
	for _, name := range []string{"Actors", "Classes", "Skills", "Items", "Weapons", "Armors", "Enemies", "Troops", "States", "Animations", "Tilesets", "CommonEvents"} {
		if err := writeJSONFile(output, spec.Directory+"/data/"+name+".json", emptyDatabase); err != nil {
			return err
		}
	}
	emptyAudio := map[string]any{"name": "", "pan": 0, "pitch": 100, "volume": 90}
	vehicle := func() map[string]any {
		return map[string]any{"bgm": emptyAudio, "characterIndex": 0, "characterName": "", "startMapId": 0, "startX": 0, "startY": 0}
	}
	system := map[string]any{
		"airship": vehicle(), "boat": vehicle(), "ship": vehicle(),
		"battleBgm": emptyAudio, "defeatMe": emptyAudio, "gameoverMe": emptyAudio,
		"titleBgm": emptyAudio, "victoryMe": emptyAudio,
		"armorTypes": []string{""}, "elements": []string{""}, "equipTypes": []string{""},
		"skillTypes": []string{""}, "weaponTypes": []string{""},
		"currencyUnit": "", "gameTitle": spec.Marker, "locale": "en_US",
		"magicSkills": []int{}, "menuCommands": []bool{false, false, false, false, false, false},
		"optDisplayTp": false, "optDrawTitle": false, "optExtraExp": false, "optFloorDeath": false,
		"optFollowers": false, "optSideView": false, "optSlipDeath": false, "optTransparent": false,
		"partyMembers": []int{}, "sounds": []any{}, "switches": []string{"", "Retrom initialized"},
		"terms": map[string]any{}, "testBattlers": []any{}, "testTroopId": 1,
		"startMapId": 1, "startX": 10, "startY": 8,
		"variables": []string{"", "Retrom fixture state"}, "versionId": 1, "windowTone": []int{0, 0, 0, 0},
	}
	if err := writeJSONFile(output, spec.Directory+"/data/System.json", system); err != nil {
		return err
	}
	mapInfos := []any{nil, map[string]any{"id": 1, "expanded": true, "name": spec.Marker, "order": 1, "parentId": 0, "scrollX": 0, "scrollY": 0}}
	if err := writeJSONFile(output, spec.Directory+"/data/MapInfos.json", mapInfos); err != nil {
		return err
	}
	gameMap := map[string]any{
		"autoplayBgm": false, "autoplayBgs": false, "battleback1Name": "", "battleback2Name": "",
		"bgm": emptyAudio, "bgs": emptyAudio, "disableDashing": false, "displayName": spec.Marker,
		"encounterList": []any{}, "encounterStep": 30, "height": 15, "note": "",
		"parallaxLoopX": false, "parallaxLoopY": false, "parallaxName": "", "parallaxShow": false,
		"parallaxSx": 0, "parallaxSy": 0, "scrollType": 0, "specifyBattleback": false,
		"tilesetId": 0, "width": 20, "data": make([]int, 20*15*6), "events": []any{nil},
	}
	return writeJSONFile(output, spec.Directory+"/data/Map001.json", gameMap)
}

func writeJSONFile(root, relative string, value any) error {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", relative, err)
	}
	contents = append(contents, '\n')
	return writeFile(root, relative, contents)
}

func digest(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}
