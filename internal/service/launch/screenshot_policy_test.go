package launch

import (
	"context"
	"errors"
	"io"
	"reflect"
	model "retrom/internal/model/launch"
	"testing"
	"time"

	"retrom/internal/adapter/files/mediaasset"
)

type screenshotMemory struct {
	initial, current                        model.ScreenshotSource
	missingInitial, missingCurrent          bool
	currentError, replaceError, commitError error
	image                                   model.ScreenshotImage
	imageError                              error
	reads, writes, transactions, identities int
	pending                                 model.ScreenshotWrite
	committed                               bool
}

func (memory *screenshotMemory) Preview(context.Context, string) (model.ScreenshotSource, bool, error) {
	return memory.initial, !memory.missingInitial, nil
}

func (memory *screenshotMemory) WithScreenshot(_ context.Context, work func(model.ScreenshotScope) error) error {
	memory.transactions++
	if err := work(memory); err != nil {
		return err
	}
	if memory.commitError != nil {
		return memory.commitError
	}
	memory.committed = true
	return nil
}

func (memory *screenshotMemory) Current(context.Context, string) (model.ScreenshotSource, bool, error) {
	return memory.current, !memory.missingCurrent, memory.currentError
}

func (memory *screenshotMemory) Replace(_ context.Context, write model.ScreenshotWrite) error {
	memory.writes++
	memory.pending = write
	return memory.replaceError
}

func (memory *screenshotMemory) Read(context.Context, io.Reader) (model.ScreenshotImage, error) {
	memory.reads++
	if memory.transactions != 0 {
		panic("image IO ran inside writer")
	}
	return memory.image, memory.imageError
}

func screenshotPolicyFixture() (*ScreenshotSaver, *screenshotMemory) {
	source := model.ScreenshotSource{
		PreviewID: "preview", ItemID: "item", SourceSnapshotID: "source", PlatformInstanceID: "directory",
		ValidationID: "validation", ProviderID: "provider", TargetID: "target",
		CredentialHash: []byte("capability"), State: "ACTIVE", HardExpiresAtMS: 200,
	}
	memory := &screenshotMemory{
		initial: source, current: source,
		image: model.ScreenshotImage{SHA256: "sha256", MediaType: "image/png", SizeBytes: 100, WidthPX: 2, HeightPX: 3},
	}
	service := NewScreenshotSaver(memory, memory, model.ScreenshotEnvironment{
		Now:     func() time.Time { return time.UnixMilli(100) },
		Matches: func(capability string, hash []byte) bool { return capability == string(hash) },
		NewID: func() (string, error) {
			memory.identities++
			return "81ed985b-3d2e-71f4-8017-e5e4ebc18c32", nil
		},
	})
	return service, memory
}

func TestScreenshotSaverAuthorizesBeforeImageAndWriter(t *testing.T) {
	cases := []struct {
		name   string
		change func(*screenshotMemory)
	}{
		{"missing", func(memory *screenshotMemory) { memory.missingInitial = true }},
		{"created", func(memory *screenshotMemory) { memory.initial.State = "CREATED" }},
		{"finished", func(memory *screenshotMemory) { memory.initial.State = "FINISHED" }},
		{"expired", func(memory *screenshotMemory) { memory.initial.HardExpiresAtMS = 100 }},
		{"credential", func(memory *screenshotMemory) { memory.initial.CredentialHash = []byte("other") }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, memory := screenshotPolicyFixture()
			test.change(memory)
			result, err := service.Store(t.Context(), "preview", "capability", nil)
			if !errors.Is(err, model.ErrCredential) || result.ID != "" || memory.reads != 0 || memory.transactions != 0 {
				t.Fatalf("result=%+v error=%v reads=%d tx=%d", result, err, memory.reads, memory.transactions)
			}
		})
	}
}

func TestScreenshotSaverRejectsChangedFinalAuthorityBeforeIdentity(t *testing.T) {
	cases := []struct {
		name   string
		change func(*screenshotMemory)
	}{
		{"missing", func(memory *screenshotMemory) { memory.missingCurrent = true }},
		{"preview", func(memory *screenshotMemory) { memory.current.PreviewID = "other" }},
		{"item", func(memory *screenshotMemory) { memory.current.ItemID = "other" }},
		{"source", func(memory *screenshotMemory) { memory.current.SourceSnapshotID = "other" }},
		{"directory", func(memory *screenshotMemory) { memory.current.PlatformInstanceID = "other" }},
		{"validation", func(memory *screenshotMemory) { memory.current.ValidationID = "other" }},
		{"provider", func(memory *screenshotMemory) { memory.current.ProviderID = "other" }},
		{"target", func(memory *screenshotMemory) { memory.current.TargetID = "other" }},
		{"credential", func(memory *screenshotMemory) { memory.current.CredentialHash = []byte("other") }},
		{"finished", func(memory *screenshotMemory) { memory.current.State = "FINISHED" }},
		{"expired", func(memory *screenshotMemory) { memory.current.HardExpiresAtMS = 100 }},
		{"extended", func(memory *screenshotMemory) { memory.current.HardExpiresAtMS = 300 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, memory := screenshotPolicyFixture()
			test.change(memory)
			result, err := service.Store(t.Context(), "preview", "capability", nil)
			if !errors.Is(err, model.ErrCredential) || result.ID != "" || memory.reads != 1 || memory.transactions != 1 || memory.identities != 0 || memory.writes != 0 {
				t.Fatalf("result=%+v error=%v reads=%d tx=%d ids=%d writes=%d", result, err, memory.reads, memory.transactions, memory.identities, memory.writes)
			}
		})
	}
}

func TestScreenshotSaverCapturesWithFinalAuthorityTime(t *testing.T) {
	service, memory := screenshotPolicyFixture()
	calls := 0
	service.environment.Now = func() time.Time {
		calls++
		return time.UnixMilli(int64(100 + calls))
	}
	result, err := service.Store(t.Context(), "preview", "capability", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := model.ReviewScreenshot{
		ID: "81ed985b-3d2e-71f4-8017-e5e4ebc18c32", ImportItemID: "item", ValidationID: "validation",
		ProviderID: "provider", TargetID: "target", WidthPX: 2, HeightPX: 3, CapturedAtMS: 102,
	}
	if result != want || calls != 2 || !memory.committed || memory.writes != 1 || memory.identities != 1 {
		t.Fatalf("result=%+v calls=%d memory=%+v", result, calls, memory)
	}
	if !reflect.DeepEqual(memory.pending.Source, memory.initial) || memory.pending.Image != memory.image || memory.pending.AtMS != result.CapturedAtMS {
		t.Fatalf("write changed authorized snapshot or inspected image: %+v", memory.pending)
	}
}

func TestScreenshotSaverDoesNotReturnUncommittedResult(t *testing.T) {
	cause := errors.New("storage unavailable")
	cases := []struct {
		name   string
		change func(*screenshotMemory)
	}{
		{"image", func(memory *screenshotMemory) { memory.imageError = cause }},
		{"final read", func(memory *screenshotMemory) { memory.currentError = cause }},
		{"replace", func(memory *screenshotMemory) { memory.replaceError = cause }},
		{"commit", func(memory *screenshotMemory) { memory.commitError = cause }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, memory := screenshotPolicyFixture()
			test.change(memory)
			result, err := service.Store(t.Context(), "preview", "capability", nil)
			if !errors.Is(err, cause) || result != (model.ReviewScreenshot{}) || memory.committed {
				t.Fatalf("result=%+v error=%v committed=%v", result, err, memory.committed)
			}
		})
	}
}

func TestScreenshotSaverRejectsInvalidImagesBeforeWriter(t *testing.T) {
	cases := []model.ScreenshotImage{
		{SizeBytes: 0, WidthPX: 2, HeightPX: 3, MediaType: "image/png"},
		{SizeBytes: mediaasset.MaxImageBytes + 1, WidthPX: 2, HeightPX: 3, MediaType: "image/png"},
		{SizeBytes: 100, WidthPX: mediaasset.MaxImagePixels, HeightPX: 2, MediaType: "image/png"},
		{SizeBytes: 100, WidthPX: 2, HeightPX: 0, MediaType: "image/png"},
		{SizeBytes: 100, WidthPX: 2, HeightPX: 3, MediaType: "image/webp"},
	}
	for _, image := range cases {
		service, memory := screenshotPolicyFixture()
		memory.image = image
		result, err := service.Store(t.Context(), "preview", "capability", nil)
		if !errors.Is(err, model.ErrReviewScreenshotInvalid) || result.ID != "" || memory.transactions != 0 {
			t.Fatalf("image=%+v result=%+v error=%v tx=%d", image, result, err, memory.transactions)
		}
	}
}

func TestScreenshotSaverRejectsUnavailableImagesBeforeAuthorization(t *testing.T) {
	service := NewScreenshotSaver(failingScreenshotRepository{cause: errors.New("must not read")}, nil, model.ScreenshotEnvironment{})
	_, err := service.Store(t.Context(), "preview", "capability", nil)
	if !errors.Is(err, model.ErrReviewScreenshotInvalid) {
		t.Fatal(err)
	}
}

func TestScreenshotSaverChecksIdentityBeforeWriting(t *testing.T) {
	for _, id := range []string{"", "invalid", "00000000-0000-0000-0000-000000000000"} {
		service, memory := screenshotPolicyFixture()
		service.environment.NewID = func() (string, error) { return id, nil }
		result, err := service.Store(t.Context(), "preview", "capability", nil)
		if err == nil || result.ID != "" || memory.writes != 0 || memory.committed {
			t.Fatalf("identity=%q result=%+v error=%v", id, result, err)
		}
	}
	service, memory := screenshotPolicyFixture()
	cause := errors.New("entropy unavailable")
	service.environment.NewID = func() (string, error) { return "", cause }
	result, err := service.Store(t.Context(), "preview", "capability", nil)
	if !errors.Is(err, cause) || result.ID != "" || memory.writes != 0 {
		t.Fatalf("identity failure result=%+v error=%v", result, err)
	}
}
