package netplay

import "retrom/internal/netplay/capability"

type Credentials = capability.Credentials

var ErrCredentialKeyInvalid = capability.ErrCredentialKeyInvalid

func LoadOrCreateCredentials(dataDir string) (*Credentials, error) {
	credentials, err := capability.LoadOrCreateCredentials(dataDir)
	if err != nil {
		return nil, serviceError("load credential key", err)
	}
	return credentials, nil
}
func EncodeCapability(value [32]byte) string { return capability.EncodeCapability(value) }
func HashCapability(value [32]byte) [32]byte { return capability.HashCapability(value) }
func MatchesCapability(encoded string, expected []byte) bool {
	return capability.MatchesCapability(encoded, expected)
}
