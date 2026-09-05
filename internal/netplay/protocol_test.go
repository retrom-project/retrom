package netplay

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"retrom/internal/testassert"

	"github.com/google/uuid"
)

func TestDecodeClientMessageRejectsUnknownDuplicateDeepAndOversizeInput(t *testing.T) {
	t.Parallel()
	valid := []byte(`{"v":1,"type":"INPUT","sessionId":"01980000-0000-7000-8000-000000000001","epoch":0,"seq":1,"frame":0,"playerNo":1,"controls":[0,0]}`)
	if message, err := DecodeClientMessage(valid); err != nil || message.Type != "INPUT" {
		t.Fatalf("valid message = %#v, %v", message, err)
	}
	invalid := [][]byte{
		[]byte(`{"v":1,"type":"INPUT","sessionId":"id","seq":1,"unknown":true}`),
		[]byte(`{"v":1,"v":1,"type":"INPUT","sessionId":"id","seq":1}`),
		[]byte(`{"v":1,"type":"INPUT","sessionId":"id","seq":1,"controls":[[[[[[[[[0]]]]]]]]]}`),
		[]byte(`{"v":1,"type":"INPUT","sessionId":"id","epoch":0,"seq":1,"frame":0,"playerNo":1,"controls":[0],"reason":"USER_EXIT"}`),
		make([]byte, MaxTextMessageBytes+1),
	}
	for index, contents := range invalid {
		if _, err := DecodeClientMessage(contents); err == nil {
			t.Fatalf("invalid message %d accepted", index)
		}
	}
}

func TestStateFrameBindsHeaderAndLength(t *testing.T) {
	t.Parallel()
	state := []byte{1, 2, 3, 4, 5}
	sessionID := uuid.MustParse("01980000-0000-7000-8000-000000000001")
	transferID := uuid.MustParse("01980000-0000-7000-8000-000000000002")
	frame := make([]byte, StateHeaderBytes+len(state))
	copy(frame[:4], "RNS1")
	copy(frame[4:20], sessionID[:])
	copy(frame[20:36], transferID[:])
	binary.BigEndian.PutUint32(frame[36:40], 7)
	binary.BigEndian.PutUint64(frame[40:48], 1234)
	// The fixture is bounded by MaxStateBytes, so the uint32 conversion cannot overflow.
	binary.BigEndian.PutUint32(frame[48:52], uint32(len(state)))
	copy(frame[52:], state)
	parsed, err := ParseStateFrame(frame)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return parsed.SessionID != sessionID }, func() bool { return parsed.Transfer != transferID }, func() bool { return parsed.Epoch != 7 }, func() bool { return parsed.NextFrame != 1234 }), "parsed frame = %#v, %v", parsed, err)
	full, core, err := StateDigests(parsed.Payload)
	want := sha256.Sum256(state)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return full != hex.EncodeToString(want[:]) }, func() bool { return core != hex.EncodeToString(want[:]) }), "digests = %s/%s, %v", full, core, err)
	frame[48] = 1
	if _, err := ParseStateFrame(frame); err == nil {
		t.Fatal("mismatched binary length accepted")
	}
}

func TestStateFrameTreatsProviderCheckpointAsOpaqueBytes(t *testing.T) {
	t.Parallel()
	state := []byte("provider-owned-checkpoint-v1\x00\x01\xff")
	sessionID := uuid.MustParse("01980000-0000-7000-8000-000000000001")
	transferID := uuid.MustParse("01980000-0000-7000-8000-000000000002")
	frame := make([]byte, StateHeaderBytes+len(state))
	copy(frame[:4], "RNS1")
	copy(frame[4:20], sessionID[:])
	copy(frame[20:36], transferID[:])
	binary.BigEndian.PutUint32(frame[36:40], 7)
	binary.BigEndian.PutUint64(frame[40:48], 1234)
	binary.BigEndian.PutUint32(frame[48:52], uint32(len(state)))
	copy(frame[52:], state)

	parsed, err := ParseStateFrame(frame)
	if err != nil {
		t.Fatalf("opaque provider state rejected: %v", err)
	}
	full, core, err := StateDigests(parsed.Payload)
	want := sha256.Sum256(state)
	wantDigest := hex.EncodeToString(want[:])
	if err != nil || full != wantDigest || core != wantDigest {
		t.Fatalf("opaque state digests = %s/%s, %v; want %s/%s", full, core, err, wantDigest, wantDigest)
	}
}
