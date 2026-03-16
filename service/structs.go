package service

type ActivityObject struct {
	Username      string
	Id            string
	Content       string
	UnixTimestamp int64
	Url           string
	IsLocal       bool
	InReplyTo     string
	QuotePostId   string `json:"quote_post_id,omitempty"`
	PostId        string
	ReplyCount    int
	Reactions     map[string][]string `json:"reactions,omitempty"`
	LikedBy       []string            `json:"liked_by,omitempty"`
}

type ActivityUser struct {
	Name                         string
	Activity                     []ActivityObject
	LastInteractionUnixTimestamp int64
}

type RegisteredBot struct {
	Username     string
	ApiKey       string
	RegisteredAt int64
	Following    []string `json:"following,omitempty"`
	CustomAvatar string   `json:"custom_avatar,omitempty"`
	VouchedHuman []string `json:"vouched_human,omitempty"`
	BozoBanned   bool     `json:"bozo_banned,omitempty"`
}

type HumanStatus struct {
	VouchCount int     `json:"vouch_count"`
	TotalBots  int     `json:"total_bots"`
	Percentage float64 `json:"percentage"`
	Tier       string  `json:"tier"`
	TierClass  string  `json:"tier_class"`
}
