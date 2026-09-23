package profilemodel

import (
	"encoding/json"
	"testing"
)

func TestRPGProfilesMapKindToOwnerModels(t *testing.T) {
	t.Parallel()
	review := &RPGReview{
		Generation: "RPGMV", EvidenceFamily: "MV", EvidenceConfidence: "MATCHED",
		Analysis: json.RawMessage(`{"selfContained":true}`), ProviderID: "retrom-runtime",
		TargetID: "rpgmaker-mv",
	}
	for _, scenario := range []struct {
		scope Scope
		model any
	}{
		{Review, review},
		{Game, review.Game()},
		{Variant, &RPGVariant{Generation: "RPGMV"}},
	} {
		encoded, err := Encode(scenario.scope, RPGMakerProject, scenario.model)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(scenario.scope, encoded)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := decoded.(*RPGReview); scenario.scope == Review && !ok {
			t.Fatalf("review decoded as %T", decoded)
		}
		if _, ok := decoded.(*RPGGame); scenario.scope == Game && !ok {
			t.Fatalf("game decoded as %T", decoded)
		}
		if _, ok := decoded.(*RPGVariant); scenario.scope == Variant && !ok {
			t.Fatalf("variant decoded as %T", decoded)
		}
	}
	if _, err := Encode(Game, RPGMakerProject, review); err == nil {
		t.Fatal("review model accepted for game profile")
	}
	if _, err := Decode(Review, `{"kind":"UNKNOWN","data":{}}`); err == nil {
		t.Fatal("unmapped kind accepted")
	}
	if _, err := Decode(Game, `{"kind":"RPG_MAKER_PROJECT","data":[]}`); err == nil {
		t.Fatal("non-object data accepted")
	}
}
