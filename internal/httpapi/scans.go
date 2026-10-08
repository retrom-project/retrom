package httpapi

import (
	"net/http"

	"retrom/internal/model"
)

func (s *Server) inspectSource(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input model.SourceInput
	if err := decode(r, &input); err != nil {
		return err
	}
	items, err := s.Scans.Inspect(r.Context(), p, input)
	if err != nil {
		return wrap(err)
	}
	return respond(w, model.List[model.SourceEntry]{Items: items})
}

func (s *Server) createScan(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input model.GameScanInput
	if err := decode(r, &input); err != nil {
		return err
	}
	scan, err := s.Scans.Create(r.Context(), p, input)
	if err != nil {
		return wrap(err)
	}
	return respond(w, scan)
}

func (s *Server) scans(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	q, err := query(r)
	if err != nil {
		return err
	}
	items, err := s.Scans.List(r.Context(), p, q)
	if err != nil {
		return wrap(err)
	}
	return respond(w, items)
}

func (s *Server) cancelScan(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	return noContent(w, s.Scans.Cancel(r.Context(), p, r.PathValue("scanId")))
}

func (s *Server) createBiosScan(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input model.BiosScanInput
	if err := decode(r, &input); err != nil {
		return err
	}
	scan, err := s.Scans.CreateBios(r.Context(), p, input)
	if err != nil {
		return wrap(err)
	}
	return respond(w, scan)
}
