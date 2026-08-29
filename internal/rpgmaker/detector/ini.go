package detector

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

func parseRPGRTINI(files *catalog) (bool, error) {
	if !files.exists("RPG_RT.ini") {
		return false, nil
	}
	contents, err := files.read("RPG_RT.ini", maxINIBytes, CodeINIInvalid)
	if err != nil {
		return false, err
	}
	contents, err = prepareRPGRTINI(contents)
	if err != nil {
		return false, err
	}
	return findFullPackageFlag(contents)
}

func prepareRPGRTINI(contents []byte) ([]byte, error) {
	if bytes.HasPrefix(contents, []byte{0xef, 0xbb, 0xbf}) {
		contents = contents[3:]
	} else if hasUTF16BOM(contents) {
		return nil, newError(CodeINIInvalid, "RPG_RT.ini has a non-UTF-8 BOM", nil)
	}
	if bytes.IndexByte(contents, 0) >= 0 {
		return nil, newError(CodeINIInvalid, "RPG_RT.ini contains NUL", nil)
	}
	return contents, nil
}

func hasUTF16BOM(contents []byte) bool {
	return len(contents) >= 2 &&
		(bytes.HasPrefix(contents, []byte{0xff, 0xfe}) || bytes.HasPrefix(contents, []byte{0xfe, 0xff}))
}

func findFullPackageFlag(contents []byte) (bool, error) {
	inSection := false
	found := false
	value := ""
	for _, rawLine := range bytes.Split(contents, []byte{'\n'}) {
		line := strings.Trim(string(bytes.TrimSuffix(rawLine, []byte{'\r'})), " \t")
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inSection = strings.EqualFold(strings.TrimSpace(line[1:len(line)-1]), "RPG_RT")
			continue
		}
		key, candidate, present := strings.Cut(line, "=")
		if !inSection || !present || !strings.EqualFold(strings.TrimSpace(key), "FullPackageFlag") {
			continue
		}
		candidate = strings.Trim(candidate, " \t")
		if !isASCII(candidate) {
			return false, newError(CodeINIInvalid, "FullPackageFlag must be ASCII", nil)
		}
		if found && candidate != value {
			return false, newError(CodeINIInvalid, "conflicting FullPackageFlag values", nil)
		}
		found = true
		value = candidate
	}
	return found && value == "1", nil
}

func decodeGameINI(contents []byte) (string, error) {
	if bytes.IndexByte(contents, 0) >= 0 {
		return "", newError(CodeINIInvalid, "Game.ini contains NUL", nil)
	}
	if bytes.HasPrefix(contents, []byte{0xef, 0xbb, 0xbf}) {
		contents = contents[3:]
		if !utf8.Valid(contents) {
			return "", newError(CodeINIEncodingUnsupported, "invalid UTF-8 after BOM", nil)
		}
		return string(contents), nil
	}
	if utf8.Valid(contents) {
		return string(contents), nil
	}
	decoded, _, err := transform.Bytes(japanese.ShiftJIS.NewDecoder(), contents)
	if err != nil || !utf8.Valid(decoded) {
		return "", newError(CodeINIEncodingUnsupported, "Game.ini is not CP932", err)
	}
	reencoded, _, err := transform.Bytes(japanese.ShiftJIS.NewEncoder(), decoded)
	if err != nil || !bytes.Equal(reencoded, contents) {
		return "", newError(CodeINIEncodingUnsupported, "Game.ini is not reversible CP932", err)
	}
	return string(decoded), nil
}

func isASCII(value string) bool {
	for index := range len(value) {
		if value[index] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
