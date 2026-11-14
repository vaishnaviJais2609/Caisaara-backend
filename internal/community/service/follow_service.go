package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/here-arjun-1/Caisaara-backend/internal/community/dto"
	"github.com/here-arjun-1/Caisaara-backend/internal/community/model"
	"github.com/here-arjun-1/Caisaara-backend/internal/notification"
)

type FollowRepository interface {
	Follow(ctx context.Context, followerID, followingID int64) error
	Unfollow(ctx context.Context, followerID, followingID int64) error
	IsFollowing(ctx context.Context, followerID, followingID int64) (bool, error)
	GetCounts(ctx context.Context, userID int64) (int, int, error)
	GetFollowers(ctx context.Context, userID int64, limit int, beforeID int64) ([]model.Author, error)
	GetFollowing(ctx context.Context, userID int64, limit int, beforeID int64) ([]model.Author, error)
	UserExists(ctx context.Context, userID int64) (bool, error)
	IsBlocked(ctx context.Context, userID, otherID int64) (bool, error)
	FindUsernameByID(ctx context.Context, userID int64) (string, error)
}

type FollowService struct {
	followRepository FollowRepository
	Notifier         Notifier
}

func NewFollowService(followRepository FollowRepository) *FollowService {
	return &FollowService{
		followRepository: followRepository,
	}
}

func (s *FollowService) SetNotifier(notifier Notifier) {
	s.Notifier = notifier
}

func (s *FollowService) Follow(ctx context.Context, followerID, targetID int64) error {
	if followerID == targetID {
		return ErrSelfFollow
	}

	exists, err := s.followRepository.UserExists(ctx, targetID)
	if err != nil {
		slog.ErrorContext(ctx, "check target user existence failed", "target_id", targetID, "error", err)
		return ErrInternal
	}
	if !exists {
		return ErrUserNotFound
	}

	blocked, err := s.followRepository.IsBlocked(ctx, followerID, targetID)
	if err != nil {
		slog.ErrorContext(ctx, "check block between users failed", "follower_id", followerID, "target_id", targetID, "error", err)
		return ErrInternal
	}
	if blocked {
		return ErrUserBlocked
	}

	wasFollowing, err := s.followRepository.IsFollowing(ctx, followerID, targetID)
	if err != nil {
		slog.ErrorContext(ctx, "check is following failed", "follower_id", followerID, "target_id", targetID, "error", err)
		return ErrInternal
	}

	if err := s.followRepository.Follow(ctx, followerID, targetID); err != nil {
		slog.ErrorContext(ctx, "follow user failed", "follower_id", followerID, "target_id", targetID, "error", err)
		return ErrInternal
	}

	if !wasFollowing {
		s.notifyFollow(followerID, targetID)
	}

	return nil
}

func (s *FollowService) notifyFollow(followerID, targetID int64) {
	if s.Notifier == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		username, err := s.followRepository.FindUsernameByID(ctx, followerID)
		if err != nil || username == "" {
			slog.Error("find follower username failed", "follower_id", followerID, "error", err)
			return
		}

		err = s.Notifier.Send(ctx, targetID, notification.TypeFollow,
			"New follower",
			"@"+username+" started following you.",
			map[string]any{"follower_id": followerID, "username": username},
		)
		if err != nil {
			slog.Error("send follow notification failed", "target_id", targetID, "error", err)
		}
	}()
}

func (s *FollowService) Unfollow(ctx context.Context, followerID, targetID int64) error {
	if followerID == targetID {
		return ErrSelfFollow
	}

	if err := s.followRepository.Unfollow(ctx, followerID, targetID); err != nil {
		slog.ErrorContext(ctx, "unfollow user failed", "follower_id", followerID, "target_id", targetID, "error", err)
		return ErrInternal
	}

	return nil
}

func (s *FollowService) IsFollowing(ctx context.Context, followerID, targetID int64) (bool, error) {
	if followerID == targetID {
		return false, nil
	}

	return s.followRepository.IsFollowing(ctx, followerID, targetID)
}

func (s *FollowService) GetCounts(ctx context.Context, userID int64) (*dto.FollowCountsResponse, error) {
	exists, err := s.followRepository.UserExists(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "check user existence for counts failed", "user_id", userID, "error", err)
		return nil, ErrInternal
	}
	if !exists {
		return nil, ErrUserNotFound
	}

	followers, following, err := s.followRepository.GetCounts(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "get follow counts failed", "user_id", userID, "error", err)
		return nil, ErrInternal
	}

	return &dto.FollowCountsResponse{
		Followers: followers,
		Following: following,
	}, nil
}

func (s *FollowService) GetFollowers(ctx context.Context, userID int64, beforeID int64, limit int) (*dto.UserListResponse, error) {
	limit = clampLimit(limit)

	exists, err := s.followRepository.UserExists(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "check user existence failed", "user_id", userID, "error", err)
		return nil, ErrInternal
	}
	if !exists {
		return nil, ErrUserNotFound
	}

	authors, err := s.followRepository.GetFollowers(ctx, userID, limit, beforeID)
	if err != nil {
		slog.ErrorContext(ctx, "get followers failed", "user_id", userID, "error", err)
		return nil, ErrInternal
	}

	users := make([]dto.AuthorResponse, 0, len(authors))
	for _, a := range authors {
		users = append(users, toAuthorDTO(a))
	}

	var nextBeforeID *int64
	if len(authors) == limit && len(authors) > 0 {
		lastID := authors[len(authors)-1].ID
		nextBeforeID = &lastID
	}

	return &dto.UserListResponse{
		Users:        users,
		NextBeforeID: nextBeforeID,
	}, nil
}

func (s *FollowService) GetFollowing(ctx context.Context, userID int64, beforeID int64, limit int) (*dto.UserListResponse, error) {
	limit = clampLimit(limit)

	exists, err := s.followRepository.UserExists(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "check user existence failed", "user_id", userID, "error", err)
		return nil, ErrInternal
	}
	if !exists {
		return nil, ErrUserNotFound
	}

	authors, err := s.followRepository.GetFollowing(ctx, userID, limit, beforeID)
	if err != nil {
		slog.ErrorContext(ctx, "get following failed", "user_id", userID, "error", err)
		return nil, ErrInternal
	}

	users := make([]dto.AuthorResponse, 0, len(authors))
	for _, a := range authors {
		users = append(users, toAuthorDTO(a))
	}

	var nextBeforeID *int64
	if len(authors) == limit && len(authors) > 0 {
		lastID := authors[len(authors)-1].ID
		nextBeforeID = &lastID
	}

	return &dto.UserListResponse{
		Users:        users,
		NextBeforeID: nextBeforeID,
	}, nil
}

func toAuthorDTO(a model.Author) dto.AuthorResponse {
	return dto.AuthorResponse{
		ID:          a.ID,
		Username:    a.Username,
		DisplayName: a.DisplayName,
		AvatarURL:   a.AvatarURL,
		IsFollowing: a.IsFollowing,
	}
}
