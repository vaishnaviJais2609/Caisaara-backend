package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/here-arjun-1/Caisaara-backend/internal/community/dto"
	"github.com/here-arjun-1/Caisaara-backend/internal/community/model"
	"github.com/here-arjun-1/Caisaara-backend/internal/notification"
	"github.com/jackc/pgx/v5"
)

type CommentRepository interface {
	CreateComment(ctx context.Context, postID, userID int64, body string) (*model.Comment, error)
	ListComments(ctx context.Context, postID, afterID int64, limit int) ([]*model.Comment, error)
	FindCommentOwners(ctx context.Context, commentID int64) (int64, int64, error)
	DeleteComment(ctx context.Context, commentID int64) error
}

type CommentService struct {
	CommentRepository CommentRepository
	PostRepository    PostRepository
	Notifier          Notifier
}

func NewCommentService(commentRepository CommentRepository, postRepository PostRepository) *CommentService {
	return &CommentService{
		CommentRepository: commentRepository,
		PostRepository:    postRepository,
	}
}

func (s *CommentService) SetNotifier(notifier Notifier) {
	s.Notifier = notifier
}

func (s *CommentService) CreateComment(ctx context.Context, userID, postID int64, req dto.CreateCommentData) (*dto.CommentResponse, error) {
	body := strings.TrimSpace(req.Body)
	length := utf8.RuneCountInString(body)
	if length < 1 || length > 500 {
		return nil, ErrInvalidComment
	}

	c, err := s.CommentRepository.CreateComment(ctx, postID, userID, body)
	if isForeignKeyError(err) {
		return nil, ErrPostNotFound
	}
	if err != nil {
		slog.Error("create comment failed", "error", err)
		return nil, ErrInternal
	}

	s.notifyComment(ctx, c)

	response := toCommentResponse(c)
	return &response, nil
}

func (s *CommentService) notifyComment(ctx context.Context, c *model.Comment) {
	if s.Notifier == nil {
		return
	}

	ownerID, err := s.PostRepository.FindPostOwner(ctx, c.PostID)
	if err != nil || ownerID == c.UserID {
		return
	}

	err = s.Notifier.Send(ctx, ownerID, notification.TypePostComment,
		"New comment",
		c.Author.Username+" commented on your post",
		map[string]any{"post_id": c.PostID, "comment_id": c.ID, "user_id": c.UserID, "username": c.Author.Username},
	)
	if err != nil {
		slog.Error("send comment notification failed", "owner_id", ownerID, "error", err)
	}
}

func (s *CommentService) ListComments(ctx context.Context, postID, afterID int64, limit int) (*dto.CommentListResponse, error) {
	limit = clampLimit(limit)

	if _, err := s.PostRepository.FindPostOwner(ctx, postID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPostNotFound
		}
		slog.Error("find post owner failed", "error", err)
		return nil, ErrInternal
	}

	comments, err := s.CommentRepository.ListComments(ctx, postID, afterID, limit)
	if err != nil {
		slog.Error("list comments failed", "error", err)
		return nil, ErrInternal
	}

	list := &dto.CommentListResponse{
		Comments: make([]dto.CommentResponse, 0, len(comments)),
	}
	for _, c := range comments {
		list.Comments = append(list.Comments, toCommentResponse(c))
	}

	if len(comments) == limit {
		lastID := comments[len(comments)-1].ID
		list.NextAfterID = &lastID
	}

	return list, nil
}

func (s *CommentService) DeleteComment(ctx context.Context, userID, commentID int64) error {
	commentOwnerID, postOwnerID, err := s.CommentRepository.FindCommentOwners(ctx, commentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCommentNotFound
	}
	if err != nil {
		slog.Error("find comment owners failed", "error", err)
		return ErrInternal
	}

	if userID != commentOwnerID && userID != postOwnerID {
		return ErrNotAllowed
	}

	if err := s.CommentRepository.DeleteComment(ctx, commentID); err != nil {
		slog.Error("delete comment failed", "error", err)
		return ErrInternal
	}

	return nil
}

func toCommentResponse(c *model.Comment) dto.CommentResponse {
	return dto.CommentResponse{
		ID: c.ID,
		Author: dto.AuthorResponse{
			ID:          c.UserID,
			Username:    c.Author.Username,
			DisplayName: c.Author.DisplayName,
			AvatarURL:   c.Author.AvatarURL,
			IsFollowing: c.Author.IsFollowing,
		},
		Body:      c.Body,
		CreatedAt: c.CreatedAt,
	}
}
