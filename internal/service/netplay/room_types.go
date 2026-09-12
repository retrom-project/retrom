package netplay

type RoomMember struct {
	MemberID        string  `json:"memberId"`
	ProfileID       string  `json:"-"`
	PlayerNo        int     `json:"playerNo"`
	Role            string  `json:"role"`
	DisplayName     string  `json:"displayName"`
	AvatarRef       *string `json:"avatarRef"`
	Ready           bool    `json:"ready"`
	ConnectionState string  `json:"connectionState"`
}

type RoomGame struct {
	GameID       string `json:"gameId"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Availability string `json:"availability"`
	PlatformName string `json:"platformName"`
	ProfileID    string `json:"profileId"`
	CoreName     string `json:"coreName"`
	ProviderID   string `json:"providerId"`
	TargetID     string `json:"targetId"`
	MaxPlayers   int    `json:"maxPlayers"`
}

type SessionSummary struct {
	SessionID string `json:"sessionId"`
	SessionNo int    `json:"sessionNo"`
	State     string `json:"state"`
}

type RoomPermissions struct {
	Host      bool `json:"host"`
	Member    bool `json:"member"`
	CanSelect bool `json:"canSelectGame"`
	CanJoin   bool `json:"canJoin"`
	CanReady  bool `json:"canReady"`
	CanStart  bool `json:"canStart"`
	CanClose  bool `json:"canClose"`
}

type Room struct {
	RoomID         string          `json:"roomId"`
	State          string          `json:"state"`
	Version        int64           `json:"version"`
	Game           *RoomGame       `json:"game"`
	Members        []RoomMember    `json:"members"`
	CurrentSession *SessionSummary `json:"currentSession"`
	Permissions    RoomPermissions `json:"permissions"`
	SelfMemberID   *string         `json:"selfMemberId"`
	ExpiresAtMS    int64           `json:"expiresAtMs"`
	ServerNowMS    int64           `json:"serverNowMs"`
	EndedAtMS      *int64          `json:"endedAtMs"`
	EndReason      *string         `json:"endReason"`
	UpdatedAtMS    int64           `json:"-"`
}
