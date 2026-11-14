package dto

import "time"

type CreatePostData struct {
	Kind   string `json:"kind" binding:"omitempty,oneof=text game bot_game position"`
	Body   string `json:"body"`
	GameID string `json:"game_id"`
	FEN    string `json:"fen"`
}

type CreateCommentData struct {
	Body string `json:"body" binding:"required"`
}

type SetReactionData struct {
	Reaction string `json:"reaction" binding:"required,oneof=like love fire wow dislike"`
}

type AuthorResponse struct {
	ID          int64   `json:"id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	IsFollowing bool    `json:"is_following"`
}

type PostResponse struct {
	ID           int64             `json:"id"`
	Author       AuthorResponse    `json:"author"`
	Kind         string            `json:"kind"`
	Body         string            `json:"body"`
	FEN          *string           `json:"fen"`
	Game         *PostGameResponse `json:"game"`
	Reactions    map[string]int    `json:"reactions"`
	MyReaction   *string           `json:"my_reaction"`
	IsBookmarked bool              `json:"is_bookmarked"`
	CommentCount int               `json:"comment_count"`
	CreatedAt    time.Time         `json:"created_at"`
}

type FeedResponse struct {
	Posts        []PostResponse `json:"posts"`
	NextBeforeID *int64         `json:"next_before_id"`
}

type CommentResponse struct {
	ID        int64          `json:"id"`
	Author    AuthorResponse `json:"author"`
	Body      string         `json:"body"`
	CreatedAt time.Time      `json:"created_at"`
}

type CommentListResponse struct {
	Comments    []CommentResponse `json:"comments"`
	NextAfterID *int64            `json:"next_after_id"`
}

type ReactionResponse struct {
	Reactions  map[string]int `json:"reactions"`
	MyReaction *string        `json:"my_reaction"`
}

type BookmarkResponse struct {
	Bookmarked bool `json:"bookmarked"`
}

type PostGameResponse struct {
	ID          string     `json:"id"`
	PGN         string     `json:"pgn"`
	WhiteName   string     `json:"white_name"`
	BlackName   string     `json:"black_name"`
	WhiteRating *int       `json:"white_rating"`
	BlackRating *int       `json:"black_rating"`
	Result      string     `json:"result"`
	EndReason   string     `json:"end_reason"`
	Mode        string     `json:"mode"`
	PlayedAt    *time.Time `json:"played_at"`
}
