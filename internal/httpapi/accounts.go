package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"retrom/internal/model"
)

type authContext struct {
	Initialized bool        `json:"initialized"`
	User        *model.User `json:"user"`
	CSRFToken   string      `json:"csrfToken"`
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	if _, err := s.Accounts.Initialized(r.Context()); err != nil {
		return wrap(err)
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

func (s *Server) authContext(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	initialized, err := s.Accounts.Initialized(r.Context())
	if err != nil {
		return wrap(err)
	}
	result := authContext{Initialized: initialized}
	p, token, err := s.principal(r)
	if err == nil {
		result.User = &p.User
		result.CSRFToken = csrf(token)
	} else if !errors.Is(err, model.ErrUnauthorized) {
		return wrap(err)
	}
	return respond(w, result)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &input); err != nil {
		return wrap(err)
	}
	user, token, err := s.Accounts.Login(r.Context(), input.Username, input.Password, s.clientIP(r))
	if err != nil {
		return wrap(err)
	}
	s.setCookie(w, token)
	return respond(w, authContext{Initialized: true, User: &user, CSRFToken: csrf(token)})
}

func (s *Server) initialize(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	var input struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
	}
	if err := decode(r, &input); err != nil {
		return wrap(err)
	}
	user,
		token,
		err := s.Accounts.Initialize(r.Context(),
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

func (s *Server) logout(w http.ResponseWriter, r *http.Request, _ model.Principal) error {
	p, token, err := s.principal(r)
	if err == nil {
		if r.Header.Get("X-Retrom-Csrf") != csrf(token) {
			return model.ErrForbidden
		}
		if err = s.Accounts.Logout(r.Context(), p); err != nil {
			return wrap(err)
		}
	} else if !errors.Is(err, model.ErrUnauthorized) {
		return wrap(err)
	}
	s.setCookie(w, "")

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) password(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decode(r, &input); err != nil {
		return wrap(err)
	}
	token, err := s.Accounts.ChangePassword(r.Context(), p, input.CurrentPassword, input.NewPassword)
	if err != nil {
		return wrap(err)
	}
	s.setCookie(w, token)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) users(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	q, err := query(r)
	if err != nil {
		return wrap(err)
	}
	result, err := s.Accounts.Users(r.Context(), p, q)
	if err != nil {
		return wrap(err)
	}
	return respond(w, result)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, p model.Principal) error {
	var input struct {
		Version     int64  `json:"version"`
		DisplayName string `json:"displayName"`
		Role        string `json:"role"`
		Status      string `json:"status"`
	}
	if err := decode(r, &input); err != nil {
		return wrap(err)
	}
	user, err := s.Accounts.UpdateUser(r.Context(), p, model.User{
		ID: r.PathValue("userId"), DisplayName: input.DisplayName,
		Version: input.Version, Role: input.Role, Status: input.Status,
	})
	if err != nil {
		return wrap(err)
	}
	return respond(w, user)
}

func query(r *http.Request) (model.Query, error) {
	q := model.Query{
		Limit: 30, Search: r.URL.Query().Get("q"), DirectoryID: r.URL.Query().Get("platformInstanceId"),
		PlatformID: r.URL.Query().Get("platformId"), TagID: r.URL.Query().Get("tagId"),
		FolderID:     r.URL.Query().Get("folderId"),
		Unclassified: r.URL.Query().Get("unclassified") == "true",
		Status:       r.URL.Query().Get("status"),
		Kind:         r.URL.Query().Get("kind"), Sort: r.URL.Query().Get("sort"),
	}
	var err error
	if value := r.URL.Query().Get("limit"); value != "" {
		q.Limit, err = strconv.Atoi(value)
		if err != nil {
			return q, model.ErrInvalid
		}
	}
	if value := r.URL.Query().Get("offset"); value != "" {
		q.Offset, err = strconv.Atoi(value)
		if err != nil {
			return q, model.ErrInvalid
		}
	}
	if q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || q.Offset > 100000 ||
		!model.BoundedText(q.Search, 240) || !model.BoundedText(q.PlatformID, 64) {
		return q, model.ErrInvalid
	}
	return q, queryTimes(r, &q)
}

func queryTimes(r *http.Request, q *model.Query) error {
	after, hasAfter, err := queryTime(r, "afterMs")
	if err != nil {
		return err
	}
	before, hasBefore, err := queryTime(r, "beforeMs")
	if err != nil {
		return err
	}
	if hasAfter && hasBefore && after > before {
		return model.ErrInvalid
	}
	if hasAfter {
		q.AfterMs = &after
	}
	if hasBefore {
		q.BeforeMs = &before
	}
	return nil
}

func queryTime(r *http.Request, name string) (int64, bool, error) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return 0, false, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, false, model.ErrInvalid
	}
	return parsed, true, nil
}
