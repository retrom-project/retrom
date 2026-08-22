package libraryimport

import (
	"context"
	"database/sql"
	"testing"

	"retrom/internal/testassert"
)

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
