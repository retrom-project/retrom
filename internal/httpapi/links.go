package httpapi

import (
	"net/http"

	"retrom/internal/model"
)

func (s *Server) invitation(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input struct {
		Role           string `json:"role"`
		ExpiresInHours int64  `json:"expiresInHours"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	link, err := s.Accounts.CreateLink(r.Context(), p, "invitation", input.Role, "", input.ExpiresInHours)
	if err != nil {
		return wrap(err)
	}
	return respond(w, link)
}

func (s *Server) resetLink(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input struct {
		ExpiresInHours int64 `json:"expiresInHours"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	link, err := s.Accounts.CreateLink(r.Context(), p, "password_reset", "", r.PathValue("userId"), input.ExpiresInHours)
	if err != nil {
		return wrap(err)
	}
	return respond(w, link)
}

func (s *Server) revokeLink(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	version, err := versionBody(r)
	if err != nil {
		return err
	}
	return noContent(w, s.Accounts.RevokeLink(r.Context(), p, r.PathValue("linkId"), version))
}

func (s *Server) inspectLink(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	var input struct {
		Token string `json:"token"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	value, err := s.Accounts.Inspect(r.Context(), input.Token, s.clientIP(r))
	if err != nil {
		return wrap(err)
	}
	return respond(w, value)
}

func (s *Server) acceptLink(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	var input struct {
		Token       string `json:"token"`
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	user,
		token,
		err := s.Accounts.Accept(r.Context(),
		input.Token,
		input.Username,
		input.DisplayName,
		input.Password,
		s.clientIP(r))
	if err != nil {
		return wrap(err)
	}
	s.setCookie(w, token)
	return respond(w, authContext{Initialized: true, User: &user, CSRFToken: csrf(token)})
}

func (s *Server) completeReset(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	var input struct {
		Token    string `json:"token"`
		Password string `json:"newPassword"`
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	return noContent(w, s.Accounts.Reset(r.Context(), input.Token, input.Password, s.clientIP(r)))
}

func (s *Server) links(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	q, err := query(r)
	if err != nil {
		return err
	}
	value, err := s.Accounts.Links(r.Context(), p, q)
	if err != nil {
		return wrap(err)
	}
	return respond(w, value)
}
