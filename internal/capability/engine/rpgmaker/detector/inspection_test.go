package detector

import (
	"errors"
	"reflect"
	"testing"
)

func TestInspectionPendingProbeIsRepeatableAndRejectsEarlyResult(t *testing.T) {
	t.Parallel()
	inspection, err := Begin("rpgmaker_2003")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inspection.Result(); !errors.Is(err, errInspectionSequence) {
		t.Fatalf("early Result error = %v", err)
	}
	if err := inspection.Index(CatalogFacts{Present: true, Files: rpg2KProject(2003).Files()}); err != nil {
		t.Fatal(err)
	}
	first, ready, err := inspection.Next()
	if err != nil || !ready {
		t.Fatalf("first probe = %#v, %t, %v", first, ready, err)
	}
	second, ready, err := inspection.Next()
	if err != nil || !ready || !reflect.DeepEqual(first, second) {
		t.Fatalf("pending probe changed: %#v / %#v, %t, %v", first, second, ready, err)
	}
	if err := inspection.Accept(makeLDBWithIDs(2003)); err != nil {
		t.Fatal(err)
	}
	next, ready, err := inspection.Next()
	if err != nil || !ready || next.File.Path != "RPG_RT.lmt" {
		t.Fatalf("next probe = %#v, %t, %v", next, ready, err)
	}
}

func TestInspectionParsesBeforeReadingAnotherFamilyOrResolvingAmbiguity(t *testing.T) {
	t.Parallel()
	project := rpg2KProject(2003)
	for name, contents := range rgssProject("Data/Scripts.rxdata") {
		project[name] = contents
	}
	for name, contents := range mvProject() {
		project[name] = contents
	}
	project["data/System.json"] = []byte("[]")
	_, err := detectFixture(VirtualCoreID, project)
	assertErrorCode(t, err, CodeWebFormatInvalid)
}

func TestInspectionFailureRetainsTypedCauseAndSelectedGeneration(t *testing.T) {
	t.Parallel()
	cause := errors.New("resource failure")
	for _, phase := range []ProbeFailurePhase{OpenFailure, ReadFailure, CloseFailure} {
		inspection, err := Begin("rpgmaker_2003")
		if err != nil {
			t.Fatal(err)
		}
		if err := inspection.Index(CatalogFacts{Present: true, Files: rpg2KProject(2003).Files()}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := inspection.Next(); err != nil {
			t.Fatal(err)
		}
		failure := inspection.Failure(phase, cause)
		var typed *Error
		if !errors.As(failure, &typed) || !errors.Is(failure, cause) || typed.ExpectedGeneration != RPG2003 {
			t.Fatalf("phase %d: failure = %v, typed = %#v", phase, failure, typed)
		}
		if _, _, err := inspection.Next(); !errors.Is(err, errInspectionSequence) {
			t.Fatalf("failed inspection resumed: %v", err)
		}
	}
}

func TestInspectionKeepsParsedEvidenceAfterCallerReusesProbeBuffer(t *testing.T) {
	t.Parallel()
	project := mvProject()
	inspection, err := Begin(VirtualCoreID)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspection.Index(CatalogFacts{Present: true, Files: project.Files()}); err != nil {
		t.Fatal(err)
	}
	for {
		probe, ready, err := inspection.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !ready {
			break
		}
		contents := append([]byte(nil), project[probe.File.Path]...)
		if err := inspection.Accept(contents); err != nil {
			t.Fatal(err)
		}
		clear(contents)
	}
	profile, err := inspection.Result()
	if err != nil || profile.EngineVersion != "1.6.2" || profile.ExpectedGeneration != RPGMV {
		t.Fatalf("reused probe buffer changed profile = %#v, %v", profile, err)
	}
}
