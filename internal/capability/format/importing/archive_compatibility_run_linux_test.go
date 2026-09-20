package importing_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	archiveadapter "retrom/internal/adapter/content/archive"
	"retrom/internal/capability/content/contentprofile"
	importing "retrom/internal/capability/format/importing"
	"retrom/internal/testkit/testsupport"
)

var errConsumerFailure = errors.New("CAPTURE_CONSUMER_FAILURE")

type zipInput struct {
	Name      string
	Bytes     []byte
	Directory bool
	WrongCRC  bool
}
type callTrace struct {
	Ordinal   int
	Path      string
	Content   importing.ArchiveContent
	ReadError string
}
type result struct {
	Name      string
	Kind      string
	Mode      string
	Entries   []importing.ArchiveEntry
	Detected  bool
	Calls     []callTrace
	Error     string
	ErrorIs   map[string]bool
	WireHex   string
	Extracted importing.ArchiveContent
}
type archiveCase struct {
	Name, Kind, Path, Mode string
	Limits                 importing.ArchiveLimits
	Canceled               bool
	Entries                []importing.ArchiveEntry
}
type capture struct {
	SourceSHA256  map[string]string
	FixtureSHA256 map[string]string
	Cases         []result
	ChildrenAfter string
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func sha(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func exactError(err error) map[string]bool {
	kinds := map[string]error{
		"unsafe": importing.ErrArchiveUnsafe, "limit": importing.ErrArchiveLimitExceeded,
		"encrypted": importing.ErrArchiveEncrypted, "volume": importing.ErrArchiveVolumeUnsupported,
		"resource": importing.ErrArchiveResourceLimit, "sandbox": importing.ErrArchiveSandboxUnavailable,
		"nested": importing.ErrNestedArchiveUnsupported, "method": importing.ErrArchiveMethodUnsupported,
		"casefold": importing.ErrArchiveCasefoldCollision, "logicalPath": importing.ErrUnsafeLogicalPath,
		"nwjs": importing.ErrNWJSExecutableInvalid, "asar": importing.ErrElectronASARInvalid,
		"canceled": context.Canceled, "deadline": context.DeadlineExceeded, "consumer": errConsumerFailure, "eof": io.EOF,
	}
	matched := map[string]bool{}
	for name, target := range kinds {
		matched[name] = errors.Is(err, target)
	}
	return matched
}

func content(reader io.Reader) (importing.ArchiveContent, error) {
	md5Hash, sha1Hash, sha256Hash, crcHash := md5.New(), sha1.New(), sha256.New(), crc32.NewIEEE()
	count, err := io.Copy(io.MultiWriter(md5Hash, sha1Hash, sha256Hash, crcHash), reader)
	return importing.ArchiveContent{Size: count, CRC32: hex.EncodeToString(crcHash.Sum(nil)), MD5: hex.EncodeToString(md5Hash.Sum(nil)), SHA1: hex.EncodeToString(sha1Hash.Sum(nil)), SHA256: hex.EncodeToString(sha256Hash.Sum(nil))}, err
}

func compatCaseContext(test archiveCase) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	if test.Mode == "expired" {
		cancel()
		ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
	}
	if test.Canceled {
		cancel()
	}
	return ctx, cancel
}

func compatConsumer(test archiveCase, outcome *result, cancel context.CancelFunc) func(importing.ArchiveEntry, io.Reader) (importing.ArchiveContent, error) {
	return func(entry importing.ArchiveEntry, reader io.Reader) (importing.ArchiveContent, error) {
		trace := callTrace{Ordinal: entry.Ordinal, Path: entry.NormalizedPath}
		if test.Mode == "fail-before" || test.Mode == "fail-unpacked" && entry.NormalizedPath == "u.node" {
			outcome.Calls = append(outcome.Calls, trace)
			return importing.ArchiveContent{}, errConsumerFailure
		}
		source := reader
		if test.Mode == "short" {
			source = io.LimitReader(reader, 1)
		}
		facts, err := content(source)
		trace.Content = facts
		trace.ReadError = errorText(err)
		outcome.Calls = append(outcome.Calls, trace)
		if test.Mode == "fail-after" {
			return facts, errConsumerFailure
		}
		if test.Mode == "bad-crc" {
			facts.CRC32 = "00000000"
		}
		if test.Mode == "empty-hashes" {
			facts.MD5 = ""
			facts.SHA1 = ""
			facts.SHA256 = ""
		}
		if test.Mode == "cancel-after-first" && len(outcome.Calls) == 1 {
			cancel()
		}
		return facts, err
	}
}

func runCase(test archiveCase) result {
	ctx, cancel := compatCaseContext(test)
	defer cancel()
	result := result{Name: test.Name, Kind: test.Kind, Mode: test.Mode}
	consumer := compatConsumer(test, &result, cancel)
	if test.Mode == "nil" {
		consumer = nil
	}
	factory := archiveadapter.New(&testsupport.DiagnosticRecorder{})
	var err error
	switch test.Kind {
	case "zip-scan":
		result.Entries, err = factory.ScanZIP(ctx, test.Path, test.Limits)
	case "zip-consume":
		result.Entries, err = compatProject(ctx, factory, test.Path, contentprofile.ArchiveZIP, test.Limits, consumer)
	case "flat":
		result.Entries, err = factory.ScanFlatZIP(ctx, test.Path, test.Limits)
	case "nwjs-validate":
		err = factory.ValidateNWJSExecutable(ctx, test.Path)
	case "nwjs-scan":
		result.Entries, err = factory.ScanNWJSExecutable(ctx, test.Path, test.Limits)
	case "nwjs-consume":
		result.Entries, err = compatProject(ctx, factory, test.Path, contentprofile.ArchiveNWJSExecutable, test.Limits, consumer)
	case "asar-detect":
		result.Detected, err = factory.DetectElectronASARZIP(ctx, test.Path, test.Limits)
	case "asar-consume":
		result.Entries, err = compatProject(ctx, factory, test.Path, contentprofile.ArchiveElectronASAR, test.Limits, consumer)
	case "7z-scan":
		result.Entries, err = importing.ScanSevenZip(ctx, test.Path, test.Limits)
	case "7z-batch":
		err = importing.ExtractSevenZipEntries(ctx, test.Path, test.Entries, func(entry importing.ArchiveEntry, reader io.Reader) error {
			_, err := consumer(entry, reader)
			return err
		})
	case "7z-extract":
		var output bytes.Buffer
		err = importing.ExtractSevenZip(ctx, test.Path, test.Entries[0], &output)
		result.Extracted, _ = content(bytes.NewReader(output.Bytes()))
	}
	result.Error = errorText(err)
	result.ErrorIs = exactError(err)
	return result
}

func protocol(ctx context.Context, executable, path string, args []string) result {
	input, err := os.Open(path)
	must(err)
	defer func() { must(input.Close()) }()
	command := exec.CommandContext(ctx, executable, append([]string{"__archive-worker"}, args...)...)
	command.ExtraFiles = []*os.File{input}
	wire, err := command.Output()
	return result{Name: "worker " + strings.Join(args, " "), Kind: "worker-wire", WireHex: hex.EncodeToString(wire), Error: errorText(err), ErrorIs: exactError(err)}
}

// compatProject replays the old consumer observations through the actual new cursor.
// The three nil-consumer inputs are separately recorded as API_REMOVED.
func compatProject(
	ctx context.Context, factory *archiveadapter.Factory, path string, format contentprofile.ArchiveFormat,
	limits importing.ArchiveLimits, consume func(importing.ArchiveEntry, io.Reader) (importing.ArchiveContent, error),
) ([]importing.ArchiveEntry, error) {
	cursor, err := factory.OpenProject(ctx, path, format, limits)
	if err != nil {
		return nil, err
	}
	closed := false
	defer func() {
		if !closed {
			must(cursor.Close())
		}
	}()
	entries := make([]importing.ArchiveEntry, 0)
	for {
		header, err := cursor.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		content, err := consume(header.Entry, cursor)
		if err != nil {
			closeErr := cursor.Close()
			closed = true
			return nil, importing.ProjectArchiveStageError(header, err, closeErr)
		}
		entry, err := cursor.Complete(content)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Ordinal < entries[right].Ordinal })
	return entries, nil
}
