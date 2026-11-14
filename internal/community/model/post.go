package model

import "time"

type Author struct {
	ID          int64
	Username    string
	DisplayName *string
	AvatarURL   *string
	IsFollowing bool
}

type UserSummary = Author

type Post struct {
	ID           int64
	UserID       int64
	Kind         string
	Body         string
	FEN          *string
	Game         *PostGame
	CreatedAt    time.Time
	Author       Author
	CommentCount int
	MyReaction   *string
	IsBookmarked bool
	Reactions    map[string]int
}

type Comment struct {
	ID        int64
	PostID    int64
	UserID    int64
	Body      string
	CreatedAt time.Time
	Author    Author
}

type PostGame struct {
	ID          string
	PGN         string
	WhiteName   string
	BlackName   string
	WhiteRating *int
	BlackRating *int
	Result      string
	EndReason   string
	Mode        string
	PlayedAt    *time.Time
}

type GameForPost struct {
	WhitePlayerID int64
	BlackPlayerID int64
	WhiteName     string
	BlackName     string
	WhiteRating   *int
	BlackRating   *int
	Status        string
	Result        string
	EndReason     string
	Mode          string
	Position      string
	EndedAt       *time.Time
	Moves         []string
}

type BotGameForPost struct {
	PlayerID    int64
	PlayerColor string
	Username    string
	BotID       string
	BotRating   int
	Status      string
	Result      string
	EndReason   string
	Position    string
	Moves       string
	EndedAt     *time.Time
}
