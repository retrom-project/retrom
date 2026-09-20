package detector

import (
	"errors"
	"fmt"
)

// CatalogFacts distinguishes an absent source from a present, empty catalog.
type CatalogFacts struct {
	Present bool
	Files   []File
}

// Probe names the one bounded input required by the current inspection stage.
type Probe struct {
	File     File
	MaxBytes int64
	Code     Code
}

type ProbeFailurePhase uint8

const (
	OpenFailure ProbeFailurePhase = iota + 1
	MissingReader
	ReadFailure
	CloseFailure
)

type inspectionStage uint8

const (
	inspectionIndex inspectionStage = iota
	inspectionRPG2K
	inspectionLDB
	inspectionLMT
	inspectionRPGINI
	inspectionRGSS
	inspectionGameINI
	inspectionWeb
	inspectionSystemJSON
	inspectionHTML
	inspectionJavaScript
	inspectionDone
	inspectionFailed
)

var errInspectionSequence = errors.New("invalid RPG Maker inspection sequence")

// Inspection contains only catalog facts and parsed evidence. Accept never keeps
// its byte input, and the caller owns all resources used to obtain each probe.
type Inspection struct {
	stage              inspectionStage
	coreID             string
	expected           Generation
	files              *catalog
	pending            Probe
	hasPending         bool
	evidence           []evidence
	current            evidence
	corePath           string
	javaScriptFiles    []catalogFile
	javaScriptPosition int
}

// Begin validates the selected core before a caller obtains catalog facts.
func Begin(coreID string) (*Inspection, error) {
	inspection := &Inspection{coreID: coreID}
	if coreID != VirtualCoreID {
		expected, err := GenerationForCore(coreID)
		if err != nil {
			return nil, err
		}
		inspection.expected = expected
	}
	return inspection, nil
}

func (inspection *Inspection) Index(facts CatalogFacts) error {
	if inspection.stage != inspectionIndex {
		return errInspectionSequence
	}
	if !facts.Present {
		return inspection.reject(newError(CodeProjectNotFound, "nil file index", nil))
	}
	files, err := newCatalog(facts.Files)
	if err != nil {
		return inspection.reject(err)
	}
	inspection.files = files
	inspection.stage = inspectionRPG2K
	return nil
}

// Next returns the pending probe unchanged until Accept consumes its bytes.
func (inspection *Inspection) Next() (Probe, bool, error) {
	if inspection.stage == inspectionIndex || inspection.stage == inspectionFailed {
		return Probe{}, false, errInspectionSequence
	}
	for !inspection.hasPending && inspection.stage != inspectionDone {
		if err := inspection.advance(); err != nil {
			return Probe{}, false, inspection.reject(err)
		}
	}
	return inspection.pending, inspection.hasPending, nil
}

func (inspection *Inspection) advance() error {
	switch inspection.stage {
	case inspectionRPG2K:
		return inspection.beginRPG2K()
	case inspectionLDB:
		return inspection.selectProbe("RPG_RT.ldb", maxLDBBytes, CodeLCFInvalid)
	case inspectionLMT:
		return inspection.selectProbe("RPG_RT.lmt", maxLMTBytes, CodeLMTInvalid)
	case inspectionRPGINI:
		if !inspection.files.exists("RPG_RT.ini") {
			inspection.finishRPG2K(false)
			return nil
		}
		return inspection.selectProbe("RPG_RT.ini", maxINIBytes, CodeINIInvalid)
	case inspectionRGSS:
		return inspection.beginRGSS()
	case inspectionGameINI:
		return inspection.selectProbe("Game.ini", maxINIBytes, CodeINIInvalid)
	case inspectionWeb:
		inspection.beginWeb()
	case inspectionSystemJSON:
		return inspection.selectProbe("data/System.json", maxSystemJSONBytes, CodeWebFormatInvalid)
	case inspectionHTML:
		return inspection.selectProbe("index.html", maxIndexHTMLBytes, CodeWebFormatInvalid)
	case inspectionJavaScript:
		return inspection.nextJavaScript()
	case inspectionIndex, inspectionDone, inspectionFailed:
		return errInspectionSequence
	default:
		return errInspectionSequence
	}
	return nil
}

func (inspection *Inspection) selectProbe(path string, limit int64, code Code) error {
	probe, err := inspection.files.probe(path, limit, code)
	if err != nil {
		return err
	}
	inspection.pending = probe
	inspection.hasPending = true
	return nil
}

func (inspection *Inspection) Accept(contents []byte) error {
	if !inspection.hasPending {
		return errInspectionSequence
	}
	if err := inspection.pending.ValidateSize(len(contents)); err != nil {
		return inspection.reject(err)
	}
	if err := inspection.accept(contents); err != nil {
		return inspection.reject(err)
	}
	inspection.pending = Probe{}
	inspection.hasPending = false
	return nil
}

func (inspection *Inspection) accept(contents []byte) error {
	switch inspection.stage {
	case inspectionLDB:
		return inspection.acceptLDB(contents)
	case inspectionLMT:
		return inspection.acceptLMT(contents)
	case inspectionRPGINI:
		selfContained, err := parseRPGRTINI(contents)
		if err != nil {
			return err
		}
		inspection.finishRPG2K(selfContained)
	case inspectionGameINI:
		evidenceSet, err := parseRGSS(inspection.files, contents)
		if err != nil {
			return err
		}
		inspection.evidence = append(inspection.evidence, evidenceSet...)
		inspection.stage = inspectionWeb
	case inspectionSystemJSON:
		if err := validateSystemJSON(contents); err != nil {
			return err
		}
		inspection.stage = inspectionHTML
	case inspectionHTML:
		if err := validateIndexHTML(contents, inspection.files); err != nil {
			return err
		}
		inspection.javaScriptFiles = inspection.files.paths()
		inspection.stage = inspectionJavaScript
	case inspectionJavaScript:
		return inspection.acceptJavaScript(contents)
	case inspectionIndex, inspectionRPG2K, inspectionRGSS, inspectionWeb, inspectionDone, inspectionFailed:
		return errInspectionSequence
	default:
		return errInspectionSequence
	}
	return nil
}

func (inspection *Inspection) Failure(phase ProbeFailurePhase, cause error) error {
	if !inspection.hasPending {
		return errInspectionSequence
	}
	return inspection.reject(inspection.pending.Failure(phase, cause))
}

func (inspection *Inspection) reject(err error) error {
	inspection.stage = inspectionFailed
	inspection.pending = Probe{}
	inspection.hasPending = false
	return withExpected(err, inspection.expected)
}

func (probe Probe) ValidateSize(length int) error {
	if int64(length) != probe.File.Size || int64(length) > probe.MaxBytes {
		return newError(probe.Code, fmt.Sprintf("size of %q changed during detection", probe.File.Path), nil)
	}
	return nil
}

func (probe Probe) Failure(phase ProbeFailurePhase, cause error) error {
	var detail string
	switch phase {
	case OpenFailure:
		detail = fmt.Sprintf("open %q", probe.File.Path)
	case MissingReader:
		detail = fmt.Sprintf("open %q returned no reader", probe.File.Path)
		cause = nil
	case ReadFailure:
		detail = fmt.Sprintf("read %q", probe.File.Path)
	case CloseFailure:
		detail = fmt.Sprintf("close %q", probe.File.Path)
	default:
		return errInspectionSequence
	}
	return newError(probe.Code, detail, cause)
}
