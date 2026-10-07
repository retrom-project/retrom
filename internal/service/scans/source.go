package scans

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	es "retrom/internal/format/emulationstation/meta"
	pg "retrom/internal/format/pegasus/meta"
	"retrom/internal/model"
	"retrom/internal/storage"
)

type (
	Candidate struct {
		Input model.GameInput
		Files []string
		Cover string
		Video string
	}
	Collection struct {
		Entry model.SourceEntry
		Games []Candidate
	}
)

func (s *Service) inspect(ctx context.Context, input model.SourceInput) ([]Collection, error) {
	if !model.OneOf(input.Format, "pegasus", "emulationstation") {
		return nil, model.ErrInvalid
	}
	root, err := s.Sources.Open(input.RootID, input.RelativePath)
	if err != nil {
		return nil, wrap(err)
	}
	defer closeRoot(root)
	entries, err := readDir(root, ".")
	if err != nil {
		return nil, err
	}
	directories := []string{"."}
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, entry.Name())
		}
	}
	result := make([]Collection, 0)
	for _, directory := range directories {
		name := "metadata.pegasus.txt"
		if input.Format == "emulationstation" {
			name = "gamelist.xml"
		}
		contents, readErr := readBounded(root, path.Join(directory, name), 8*1024*1024)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			return nil, readErr
		}
		parsed, parseErr := parse(ctx, input.Format, directory, contents)
		if parseErr != nil {
			return nil, parseErr
		}
		result = append(result, parsed...)
	}
	if len(result) == 0 {
		return nil, model.ErrInvalid
	}
	return result, nil
}

func parse(ctx context.Context, format, directory string, contents []byte) ([]Collection, error) {
	if format == "pegasus" {
		return parsePegasus(directory, contents)
	}
	return parseGamelist(ctx, directory, contents)
}

func parsePegasus(directory string, contents []byte) ([]Collection, error) {
	document, err := pg.Parse(contents)
	if err != nil {
		return nil, fmt.Errorf("pegasus metadata: %w", model.ErrInvalid)
	}
	result := make([]Collection, 0, len(document.Collections))
	for _, collection := range document.Collections {
		item := Collection{
			Entry: model.SourceEntry{
				Key:          directory + ":" + strconv.Itoa(collection.SegmentOrdinal),
				Name:         collection.Name,
				RelativePath: directory,
			},
			Games: make([]Candidate,
				0,
				len(collection.Games)),
		}
		for _, game := range collection.Games {
			candidate := Candidate{
				Input: metadata(game.Metadata.Title,
					game.Metadata.Description,
					game.Metadata.Developer,
					game.Metadata.Publisher,
					game.Metadata.Genre,
					game.Metadata.Players,
					game.Metadata.ReleaseYear),
				Files: game.Files,
			}
			if game.BlockedCode != "" {
				candidate.Files = nil
			}
			if len(game.Assets.Covers) > 0 {
				candidate.Cover = game.Assets.Covers[0]
			}
			if len(game.Assets.Videos) > 0 {
				candidate.Video = game.Assets.Videos[0]
			}
			item.Games = append(item.Games, candidate)
		}
		item.Entry.GameCount = int64(len(item.Games))
		result = append(result, item)
	}
	return result, nil
}

func parseGamelist(ctx context.Context, directory string, contents []byte) ([]Collection, error) {
	document, err := es.ParseContext(ctx, contents, 9999)
	if err != nil {
		return nil, fmt.Errorf("gamelist metadata: %w", model.ErrInvalid)
	}
	item := Collection{
		Entry: model.SourceEntry{
			Key:          directory,
			Name:         path.Base(directory),
			RelativePath: directory,
		},
		Games: make([]Candidate,
			0,
			len(document.Games)),
	}
	for _, game := range document.Games {
		candidate := Candidate{
			Input: metadata(game.Metadata.Title,
				game.Metadata.Description,
				game.Metadata.Developer,
				game.Metadata.Publisher,
				game.Metadata.Genre,
				game.Metadata.Players,
				game.Metadata.ReleaseYear),
			Files: []string{game.Path},
		}
		if game.BlockedCode != "" {
			candidate.Files = nil
		}
		if game.Assets.Cover != nil {
			candidate.Cover = game.Assets.Cover.RelativePath
		}
		if game.Assets.Video != nil {
			candidate.Video = game.Assets.Video.RelativePath
		}
		item.Games = append(item.Games, candidate)
	}
	item.Entry.GameCount = int64(len(item.Games))
	return []Collection{item}, nil
}

func metadata(title, description, developer, publisher, genre string, players *string, year *int) model.GameInput {
	input := model.GameInput{
		Title:       title,
		Description: description,
		Developer:   developer,
		Publisher:   publisher,
		Genre:       genre,
		TagIDs:      []string{},
	}
	input.Players = players
	if year != nil {
		value := int64(*year)
		input.ReleaseYear = &value
	}
	return input
}

func declared(base, value string) (string, error) {
	value = strings.TrimPrefix(value, "./")
	if !storage.SafeRelative(value) {
		return "", model.ErrInvalid
	}
	return path.Join(base, value), nil
}

func readBounded(root *os.Root, name string, limit int64) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open source metadata: %w", err)
	}
	defer closeFile(file)
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read source metadata: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, model.ErrInvalid
	}
	return data, nil
}

func readDir(root *os.Root, name string) ([]os.DirEntry, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open source directory: %w", err)
	}
	defer closeFile(file)
	entries, err := file.ReadDir(10001)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read source tree: %w", err)
	}
	if len(entries) > 10000 {
		return nil, model.ErrInvalid
	}
	return entries, nil
}
