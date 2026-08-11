package libraryimport

import (
	"context"
	"database/sql"
	"testing"
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
			if len(dispositions) != 1 || dispositions[0].disposition != "SOURCE" || dispositions[0].reason != "" ||
				len(groups) != 1 || len(groups[0].sources) != 1 || groups[0].sources[0].logicalName != test.logicalName ||
				len(archives) != 0 {
				t.Fatalf("admission = dispositions:%#v groups:%#v archives:%#v", dispositions, groups, archives)
			}
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
	if len(dispositions) != 1 || dispositions[0].disposition != "REJECTED" ||
		dispositions[0].reason != "UNSUPPORTED_CONTENT_FORMAT" || len(groups) != 0 || len(archives) != 0 {
		t.Fatalf("unexpected unsupported admission = dispositions:%#v groups:%#v archives:%#v", dispositions, groups, archives)
	}
}
