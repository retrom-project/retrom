package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"retrom/internal/contentcapability"
	"retrom/internal/testassert"
)

func TestRPGMakerStandardArchiveAdmissionCanonicalizesToProjectDetection(t *testing.T) {
	t.Parallel()
	for _, logicalName := range []string{"Lucky.easyrpg.zip", "Game.7z"} {
		t.Run(logicalName, func(t *testing.T) {
			t.Parallel()
			request, mode, err := normalizeTargetCreateRequest(
				CreateRequest{ContentMode: contentcapability.ModeStandard, MetadataProvider: "HASHEOUS"},
				contentcapability.ModeStandard,
				"GENERAL",
				"FILES",
				[]importSourceFile{{path: logicalName}},
				creationTarget{platformID: "rpgmaker"},
			)
			if err != nil || mode != contentcapability.ModeRPGMakerProject ||
				request.ContentMode != contentcapability.ModeRPGMakerProject || request.MetadataProvider != "NONE" {
				t.Fatalf("normalized request/mode = %#v/%q, error=%v", request, mode, err)
			}
		})
	}
}

func TestRPGMakerStandardFileAdmissionRejectsNonProjectShapes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		sourceType string
		files      []importSourceFile
	}{
		{name: "multiple archives", sourceType: "FILES", files: []importSourceFile{{path: "one.zip"}, {path: "two.zip"}}},
		{name: "unsupported extension", sourceType: "FILES", files: []importSourceFile{{path: "game.exe"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := normalizeTargetCreateRequest(
				CreateRequest{ContentMode: contentcapability.ModeStandard, MetadataProvider: "NONE"},
				contentcapability.ModeStandard,
				"GENERAL",
				test.sourceType,
				test.files,
				creationTarget{platformID: "rpgmaker"},
			)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("normalizeTargetCreateRequest() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestStandardArchiveAdmissionDoesNotRewriteOtherPlatforms(t *testing.T) {
	t.Parallel()
	want := CreateRequest{ContentMode: contentcapability.ModeStandard, MetadataProvider: "HASHEOUS"}
	request, mode, err := normalizeTargetCreateRequest(
		want,
		contentcapability.ModeStandard,
		"GENERAL",
		"FILES",
		[]importSourceFile{{path: "game.zip"}},
		creationTarget{platformID: "nes"},
	)
	if err != nil || request.ContentMode != want.ContentMode ||
		request.MetadataProvider != want.MetadataProvider || mode != contentcapability.ModeStandard {
		t.Fatalf("non-RPG request/mode = %#v/%q, error=%v", request, mode, err)
	}
}

func TestExpandedPlatformsAdmitTheirVerifiedRawExtensions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		platformID  string
		logicalName string
	}{
		{platformID: "virtualboy", logicalName: "Panic Bomber.VB"},
		{platformID: "wonderswan", logicalName: "Mingle Magnet.ws"},
		{platformID: "wonderswan", logicalName: "WonderSwan Color.wsc"},
		{platformID: "mastersystem", logicalName: "Bank Panic.sms"},
		{platformID: "nintendo3ds", logicalName: "Cave Story 2D.3ds"},
		{platformID: "nintendo3ds", logicalName: "Cave Story 2D.cci"},
	}
	service := &Service{}
	for _, test := range tests {
		t.Run(test.platformID+"/"+test.logicalName, func(t *testing.T) {
			dispositions, groups, archives := service.prepareImportFiles(
				context.Background(),
				test.platformID,
				"FILES",
				[]importSourceFile{{id: "fixture", path: test.logicalName, blobID: "blob", sha256: "digest", size: 1}},
				sql.NullString{},
			)
			testassert.Falsef(t, testassert.Any(func() bool { return len(dispositions) != 1 }, func() bool { return dispositions[0].disposition != "SOURCE" }, func() bool { return dispositions[0].reason != "" }, func() bool { return len(groups) != 1 }, func() bool { return len(groups[0].sources) != 1 }, func() bool { return groups[0].sources[0].logicalName != test.logicalName }, func() bool { return len(archives) != 0 }), "admission = dispositions:%#v groups:%#v archives:%#v", dispositions, groups, archives)
		})
	}
}

func TestExpandedPlatformsRejectUnregisteredRawExtensions(t *testing.T) {
	t.Parallel()
	dispositions, groups, archives := (&Service{}).prepareImportFiles(
		context.Background(),
		"nintendo3ds",
		"FILES",
		[]importSourceFile{{id: "fixture", path: "game.3dsx", blobID: "blob", sha256: "digest", size: 1}},
		sql.NullString{},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return len(dispositions) != 1 }, func() bool { return dispositions[0].disposition != "REJECTED" }, func() bool { return dispositions[0].reason != "UNSUPPORTED_CONTENT_FORMAT" }, func() bool { return len(groups) != 0 }, func() bool { return len(archives) != 0 }), "unexpected unsupported admission = dispositions:%#v groups:%#v archives:%#v", dispositions, groups, archives)
}
