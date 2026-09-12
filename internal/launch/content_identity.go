package launch

import (
	"fmt"

	application "retrom/internal/service/launch"
)

const RuntimeContentPath = application.RuntimeContentPath

func ContentIdentity(content ContentView) (string, error) {
	identity, err := application.ContentIdentity(content)
	if err != nil {
		return "", fmt.Errorf("content identity: %w", err)
	}
	return identity, nil
}

func ExternalContentIdentity(digest string) (string, error) {
	identity, err := application.ExternalContentIdentity(digest)
	if err != nil {
		return "", fmt.Errorf("external content identity: %w", err)
	}
	return identity, nil
}

func BundleIdentity(files []BundleFile) (string, error) {
	identity, err := application.BundleIdentity(files)
	if err != nil {
		return "", fmt.Errorf("bundle identity: %w", err)
	}
	return identity, nil
}

func RuntimeContentURL(kind, identity, logicalName string) (string, error) {
	value, err := application.RuntimeContentURL(kind, identity, logicalName)
	if err != nil {
		return "", fmt.Errorf("runtime content URL: %w", err)
	}
	return value, nil
}
