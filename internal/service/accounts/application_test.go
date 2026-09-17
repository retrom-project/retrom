package accounts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"testing"
	"time"

	model "retrom/internal/model/accounts"

	"retrom/internal/bootstrap/config"
)

func TestApplicationContextSeparatesPendingReadyAndSessionFailures(t *testing.T) {
	initialization := &initializationMemory{state: model.InitializationState{State: "PENDING"}}
	authentication := validAuthMemory()
	service := New(Modules{Initialization: NewInitialization(initialization, model.InitializationOptions{}), Authentication: NewAuthentication(authentication, nil, nil, "", func() time.Time { return time.UnixMilli(100) })}, config.ModeTest)
	pending, err := service.Context(t.Context(), "")
	if err != nil || pending.InstanceState != "INITIALIZATION_REQUIRED" || pending.Session != nil {
		t.Fatalf("pending context: %+v %v", pending, err)
	}
	initialization.state = model.InitializationState{State: "COMPLETED", TestDefault: true}
	ready, err := service.Context(t.Context(), "")
	if err != nil || ready.InstanceState != "READY" || !ready.TestDefaultAccountActive || ready.Session != nil {
		t.Fatalf("ready context: %+v %v", ready, err)
	}
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	authenticated, err := service.Context(t.Context(), token)
	if err != nil || authenticated.Session == nil {
		t.Fatalf("authenticated context: %+v %v", authenticated, err)
	}
	authentication.found = false
	missing, err := service.Context(t.Context(), token)
	if err != nil || missing.Session != nil || missing.InstanceState != "READY" {
		t.Fatalf("missing session: %+v %v", missing, err)
	}
	authentication.readError = context.Canceled
	if _, err := service.Context(t.Context(), token); !errors.Is(err, context.Canceled) {
		t.Fatalf("hidden storage cause: %v", err)
	}
}

func TestMintSessionPreservesEntropyFailureAndStoresOnlyHash(t *testing.T) {
	failed, err := MintSession(bytes.NewReader(make([]byte, 4)))
	if !errors.Is(err, io.ErrUnexpectedEOF) || failed.ID != "" || failed.Token != "" {
		t.Fatalf("entropy failure: %+v %v", failed, err)
	}
	raw := bytes.Repeat([]byte{0x42}, 32)
	material, err := MintSession(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if material.ID == "" || material.Token != base64.RawURLEncoding.EncodeToString(raw) || material.Hash != sha256.Sum256(raw) {
		t.Fatal("invalid session material")
	}
}
