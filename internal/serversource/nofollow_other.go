//go:build !linux

package serversource

import (
	"io/fs"
	"os"
)

func openDirectoryNoFollow(string) (*os.File, error)       { return nil, ErrRootUnavailable }
func openDirectoryAt(*os.File, string) (*os.File, error)   { return nil, ErrRootUnavailable }
func openRegularFileAt(*os.File, string) (*os.File, error) { return nil, ErrRootUnavailable }
func inspectEntryAt(*os.File, string) (fs.FileInfo, error) { return nil, ErrRootUnavailable }
func duplicateDirectory(*os.File) (*os.File, error)        { return nil, ErrRootUnavailable }
func systemFacts(any) any                                  { return nil }
