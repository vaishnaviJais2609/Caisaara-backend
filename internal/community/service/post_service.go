package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/corentings/chess/v2"
	"github.com/here-arjun-1/Caisaara-backend/internal/community/dto"
	"github.com/here-arjun-1/Caisaara-backend/internal/community/model"
	"github.com/here-arjun-1/Caisaara-backend/internal/notification"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	defaultPageSize             = 20
	maxPageSize                 = 50
	postgresForeignKeyViolation = "23503"
)

type PostRepository interface {
	CreatePost(ctx context.Context, post *model.Post) (int64, error)
	FindPostByID(ctx context.Context, viewerID, postID int64) (*model.Post, error)
	ListPosts(ctx context.Context, viewerID, beforeID int64, limit int) ([]*model.Post, error)
	FindPostOwner(ctx context.Context, postID int64) (int64, error)
	DeletePost(ctx context.Context, postID int64) error
	CountReactions(ctx context.Context, postIDs []int64) (map[int64]map[string]int, error)
	SetReaction(ctx context.Context, postID, userID int64, reaction string) (bool, error)
	HasRecentReactionNotification(ctx context.Context, ownerID, postID, userID int64) (bool, error)
	DeleteReaction(ctx context.Context, postID, userID int64) error
	ListBookmarkedPosts(ctx context.Context, userID, beforeID int64, limit int) ([]*model.Post, error)
	AddBookmark(ctx context.Context, postID, userID int64) error
	RemoveBookmark(ctx context.Context, postID, userID int64) error
	FindUsernameByID(ctx context.Context, userID int64) (string, error)
}

type Notifier interface {
	Send(ctx context.Context, userID int64, notificationType string, title string, body string, data map[string]any) error
}

type GameRepository interface {
	FindGame(ctx context.Context, gameID string) (*model.GameForPost, error)
	FindBotGame(ctx context.Context, gameID string) (*model.BotGameForPost, error)
}

type PostService struct {
	PostRepository PostRepository
	GameRepository GameRepository
	Notifier       Notifier
}

func NewPostService(postRepository PostRepository, gameRepository GameRepository) *PostService {
	return &PostService{
		PostRepository: postRepository,
		GameRepository: gameRepository,
	}
}

func (s *PostService) CreatePost(ctx context.Context, userID int64, req dto.CreatePostData) (*dto.PostResponse, error) {
	kind := req.Kind
	if kind == "" {
		kind = "text"
	}

	body := strings.TrimSpace(req.Body)
	length := utf8.RuneCountInString(body)
	if length > 2000 || (kind == "text" && length < 1) {
		return nil, ErrInvalidBody
	}

	post := &model.Post{
		UserID: userID,
		Kind:   kind,
		Body:   body,
	}

	switch kind {
	case "position":
		fen := strings.TrimSpace(req.FEN)
		if fen == "" || len(fen) > 100 {
			return nil, ErrInvalidFEN
		}
		if _, err := chess.FEN(fen); err != nil {
			return nil, ErrInvalidFEN
		}
		post.FEN = &fen
	case "game":
		if err := s.attachGame(ctx, post, req.GameID); err != nil {
			return nil, err
		}
	case "bot_game":
		if err := s.attachBotGame(ctx, post, req.GameID); err != nil {
			return nil, err
		}
	}

	postID, err := s.PostRepository.CreatePost(ctx, post)
	if err != nil {
		slog.Error("create post failed", "error", err)
		return nil, ErrInternal
	}

	return s.GetPost(ctx, userID, postID)
}

func (s *PostService) GetPost(ctx context.Context, viewerID, postID int64) (*dto.PostResponse, error) {
	p, err := s.PostRepository.FindPostByID(ctx, viewerID, postID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPostNotFound
	}
	if err != nil {
		slog.Error("find post failed", "error", err)
		return nil, ErrInternal
	}

	counts, err := s.PostRepository.CountReactions(ctx, []int64{p.ID})
	if err != nil {
		slog.Error("count reactions failed", "error", err)
		return nil, ErrInternal
	}
	p.Reactions = counts[p.ID]

	response := toPostResponse(p)
	return &response, nil
}

func (s *PostService) GetFeed(ctx context.Context, viewerID, beforeID int64, limit int) (*dto.FeedResponse, error) {
	limit = clampLimit(limit)

	posts, err := s.PostRepository.ListPosts(ctx, viewerID, beforeID, limit)
	if err != nil {
		slog.Error("list posts failed", "error", err)
		return nil, ErrInternal
	}

	return s.buildFeed(ctx, posts, limit)
}

func (s *PostService) GetBookmarks(ctx context.Context, userID, beforeID int64, limit int) (*dto.FeedResponse, error) {
	limit = clampLimit(limit)

	posts, err := s.PostRepository.ListBookmarkedPosts(ctx, userID, beforeID, limit)
	if err != nil {
		slog.Error("list bookmarked posts failed", "error", err)
		return nil, ErrInternal
	}

	return s.buildFeed(ctx, posts, limit)
}

func (s *PostService) buildFeed(ctx context.Context, posts []*model.Post, limit int) (*dto.FeedResponse, error) {
	postIDs := make([]int64, 0, len(posts))
	for _, p := range posts {
		postIDs = append(postIDs, p.ID)
	}

	counts, err := s.PostRepository.CountReactions(ctx, postIDs)
	if err != nil {
		slog.Error("count reactions failed", "error", err)
		return nil, ErrInternal
	}

	feed := &dto.FeedResponse{
		Posts: make([]dto.PostResponse, 0, len(posts)),
	}
	for _, p := range posts {
		p.Reactions = counts[p.ID]
		feed.Posts = append(feed.Posts, toPostResponse(p))
	}

	if len(posts) == limit {
		lastID := posts[len(posts)-1].ID
		feed.NextBeforeID = &lastID
	}

	return feed, nil
}

func (s *PostService) DeletePost(ctx context.Context, userID, postID int64) error {
	ownerID, err := s.PostRepository.FindPostOwner(ctx, postID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPostNotFound
	}
	if err != nil {
		slog.Error("find post owner failed", "error", err)
		return ErrInternal
	}

	if ownerID != userID {
		return ErrNotAllowed
	}

	if err := s.PostRepository.DeletePost(ctx, postID); err != nil {
		slog.Error("delete post failed", "error", err)
		return ErrInternal
	}

	return nil
}

func (s *PostService) SetNotifier(notifier Notifier) {
	s.Notifier = notifier
}

func (s *PostService) SetReaction(ctx context.Context, userID, postID int64, reaction string) (*dto.ReactionResponse, error) {
	isNew, err := s.PostRepository.SetReaction(ctx, postID, userID, reaction)
	if isForeignKeyError(err) {
		return nil, ErrPostNotFound
	}
	if err != nil {
		slog.Error("set reaction failed", "error", err)
		return nil, ErrInternal
	}

	if isNew && reaction != "dislike" {
		s.notifyReaction(ctx, userID, postID)
	}
	return s.reactionSummary(ctx, postID, &reaction)
}

func (s *PostService) notifyReaction(ctx context.Context, userID, postID int64) {
	if s.Notifier == nil {
		return
	}

	ownerID, err := s.PostRepository.FindPostOwner(ctx, postID)
	if err != nil || ownerID == userID {
		return
	}

	hasRecent, err := s.PostRepository.HasRecentReactionNotification(ctx, ownerID, postID, userID)
	if err != nil {
		slog.Error("check recent reaction notification failed", "owner_id", ownerID, "error", err)
		return
	}
	if hasRecent {
		return
	}

	username, err := s.PostRepository.FindUsernameByID(ctx, userID)
	if err != nil {
		return
	}

	err = s.Notifier.Send(ctx, ownerID, notification.TypePostReaction,
		"New reaction",
		username+" reacted to your post",
		map[string]any{"post_id": postID, "user_id": userID, "username": username},
	)
	if err != nil {
		slog.Error("send reaction notification failed", "owner_id", ownerID, "error", err)
	}
}

func (s *PostService) DeleteReaction(ctx context.Context, userID, postID int64) (*dto.ReactionResponse, error) {
	if _, err := s.PostRepository.FindPostOwner(ctx, postID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPostNotFound
		}
		slog.Error("find post owner failed", "error", err)
		return nil, ErrInternal
	}

	if err := s.PostRepository.DeleteReaction(ctx, postID, userID); err != nil {
		slog.Error("delete reaction failed", "error", err)
		return nil, ErrInternal
	}

	return s.reactionSummary(ctx, postID, nil)
}

func (s *PostService) reactionSummary(ctx context.Context, postID int64, myReaction *string) (*dto.ReactionResponse, error) {
	counts, err := s.PostRepository.CountReactions(ctx, []int64{postID})
	if err != nil {
		slog.Error("count reactions failed", "error", err)
		return nil, ErrInternal
	}

	return &dto.ReactionResponse{
		Reactions:  nonNilCounts(counts[postID]),
		MyReaction: myReaction,
	}, nil
}

func toPostResponse(p *model.Post) dto.PostResponse {
	var game *dto.PostGameResponse
	if p.Game != nil {
		game = &dto.PostGameResponse{
			ID:          p.Game.ID,
			PGN:         p.Game.PGN,
			WhiteName:   p.Game.WhiteName,
			BlackName:   p.Game.BlackName,
			WhiteRating: p.Game.WhiteRating,
			BlackRating: p.Game.BlackRating,
			Result:      p.Game.Result,
			EndReason:   p.Game.EndReason,
			Mode:        p.Game.Mode,
			PlayedAt:    p.Game.PlayedAt,
		}
	}

	return dto.PostResponse{
		ID: p.ID,
		Author: dto.AuthorResponse{
			ID:          p.Author.ID,
			Username:    p.Author.Username,
			DisplayName: p.Author.DisplayName,
			AvatarURL:   p.Author.AvatarURL,
			IsFollowing: p.Author.IsFollowing,
		},
		Kind:         p.Kind,
		Body:         p.Body,
		FEN:          p.FEN,
		Game:         game,
		Reactions:    nonNilCounts(p.Reactions),
		MyReaction:   p.MyReaction,
		IsBookmarked: p.IsBookmarked,
		CommentCount: p.CommentCount,
		CreatedAt:    p.CreatedAt,
	}
}

func nonNilCounts(counts map[string]int) map[string]int {
	if counts == nil {
		return map[string]int{}
	}
	return counts
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultPageSize
	}
	if limit > maxPageSize {
		return maxPageSize
	}
	return limit
}

func isForeignKeyError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == postgresForeignKeyViolation
}

func (s *PostService) AddBookmark(ctx context.Context, userID, postID int64) (*dto.BookmarkResponse, error) {
	err := s.PostRepository.AddBookmark(ctx, postID, userID)
	if isForeignKeyError(err) {
		return nil, ErrPostNotFound
	}
	if err != nil {
		slog.Error("add bookmark failed", "error", err)
		return nil, ErrInternal
	}

	return &dto.BookmarkResponse{Bookmarked: true}, nil
}

func (s *PostService) RemoveBookmark(ctx context.Context, userID, postID int64) (*dto.BookmarkResponse, error) {
	if _, err := s.PostRepository.FindPostOwner(ctx, postID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPostNotFound
		}
		slog.Error("find post owner failed", "error", err)
		return nil, ErrInternal
	}

	if err := s.PostRepository.RemoveBookmark(ctx, postID, userID); err != nil {
		slog.Error("remove bookmark failed", "error", err)
		return nil, ErrInternal
	}

	return &dto.BookmarkResponse{Bookmarked: false}, nil
}
