package model

type User struct {
	ID             string `json:"id"`
	Username       string `json:"username"`
	DisplayName    string `json:"displayName"`
	Role           string `json:"role"`
	Status         string `json:"status"`
	Version        int64  `json:"version"`
	CreatedAtMs    int64  `json:"createdAtMs"`
	LastLoginAtMs  *int64 `json:"lastLoginAtMs"`
	SessionVersion int64  `json:"-"`
}

type AccountLink struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Role         string `json:"role"`
	URL          string `json:"url"`
	ExpiresAtMs  int64  `json:"expiresAtMs"`
	Version      int64  `json:"version"`
	TargetUserID string `json:"-"`
}

type Principal struct {
	User      User
	SessionID string
}

func (p Principal) Admin() error {
	if p.User.Role != "admin" || p.User.Status != "active" {
		return ErrForbidden
	}
	return nil
}

type AccountLinkSummary struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Status      string  `json:"status"`
	Role        *string `json:"role"`
	ExpiresAtMs int64   `json:"expiresAtMs"`
	CreatedAtMs int64   `json:"createdAtMs"`
	Version     int64   `json:"version"`
}
