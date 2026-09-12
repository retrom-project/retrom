package accounts

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func normalizeUserQuery(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrUserQuery
	}
	value = norm.NFC.String(strings.TrimFunc(value, unicode.IsSpace))
	count := 0
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", ErrUserQuery
		}
		count++
	}
	if count > 80 {
		return "", ErrUserQuery
	}
	return value, nil
}

func directoryQuery(filter UserListFilter) (UserQuery, error) {
	text, err := normalizeUserQuery(filter.Query)
	if err != nil {
		return UserQuery{}, err
	}
	filter, err = directoryDefaults(filter)
	if err != nil {
		return UserQuery{}, err
	}
	query := UserQuery{Text: text, Role: filter.Role, Status: filter.Status, Sort: filter.Sort, Limit: filter.Limit}
	after, err := directoryCursor(filter)
	if err != nil {
		return UserQuery{}, err
	}
	if filter.AfterID != "" {
		query.After = &after
	}
	return query, nil
}

func validDirectoryStatus(status string) bool {
	switch status {
	case "NON_DELETED", "ALL", "ENABLED", "DISABLED", "DELETED":
		return true
	default:
		return false
	}
}

func directoryCursor(filter UserListFilter) (UserCursor, error) {
	if filter.AfterID == "" && len(filter.AfterValues) == 0 {
		return UserCursor{}, nil
	}
	after := UserCursor{ID: filter.AfterID}
	expected := 1
	if filter.Sort == "LAST_LOGIN_DESC" {
		expected = 2
	}
	if filter.AfterID == "" || len(filter.AfterValues) != expected {
		return after, ErrUserQuery
	}
	switch filter.Sort {
	case "USERNAME_ASC":
		after.Username = filter.AfterValues[0]
		if after.Username == "" {
			return after, ErrUserQuery
		}
	case "CREATED_DESC":
		value, err := strconv.ParseInt(filter.AfterValues[0], 10, 64)
		if err != nil || value < 0 {
			return after, ErrUserQuery
		}
		after.CreatedAt = value
	case "LAST_LOGIN_DESC":
		return lastLoginCursor(after, filter.AfterValues)
	}
	return after, nil
}

func directoryDefaults(filter UserListFilter) (UserListFilter, error) {
	switch filter.Role {
	case "", "ADMIN", "USER":
	default:
		return filter, ErrUserQuery
	}
	if filter.Status == "" {
		filter.Status = "NON_DELETED"
	}
	if !validDirectoryStatus(filter.Status) {
		return filter, ErrUserQuery
	}
	if filter.Sort == "" {
		filter.Sort = "CREATED_DESC"
	}
	switch filter.Sort {
	case "CREATED_DESC", "USERNAME_ASC", "LAST_LOGIN_DESC":
	default:
		return filter, ErrUserQuery
	}
	if filter.Limit == 0 {
		filter.Limit = 51
	}
	if filter.Limit < 1 || filter.Limit > 101 {
		return filter, ErrUserQuery
	}
	return filter, nil
}

func lastLoginCursor(after UserCursor, values []string) (UserCursor, error) {
	last, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || last < -1 {
		return after, ErrUserQuery
	}
	created, err := strconv.ParseInt(values[1], 10, 64)
	if err != nil || created < 0 {
		return after, ErrUserQuery
	}
	after.LastLogin = last
	after.CreatedAt = created
	after.NeverLoggedIn = last == -1
	return after, nil
}
