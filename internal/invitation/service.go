package invitation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/here-arjun-1/Caisaara-backend/internal/auth/model"
	"github.com/here-arjun-1/Caisaara-backend/internal/game"
	"github.com/redis/go-redis/v9"
)

type UserFinder interface {
	FindUserByID(id int64) (*model.User, error)
}

type InvitationService interface {
	CreateInvite(ctx context.Context, userID int64, timeControlMinutes int, color string) (*Invite, error)
	GetInvite(ctx context.Context, code string) (*Invite, error)
	JoinInvite(ctx context.Context, code string, player2ID int64) (string, error)
	FindInviteCreator(ctx context.Context, creatorID int64) (*model.User, error)
}

type Service struct {
	InviteRepo     InviteRepository
	GameRepository game.GameRepository
	UserFinder     UserFinder
}

func NewService(
	inviteRepo InviteRepository,
	gameRepository game.GameRepository,
	userFinder UserFinder,
) *Service {
	return &Service{
		InviteRepo:     inviteRepo,
		GameRepository: gameRepository,
		UserFinder:     userFinder,
	}
}

func generateInviteCode() (string, error) {
	b := make([]byte, 4)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Service) CreateInvite(
	ctx context.Context,
	userID int64,
	timeControlMinutes int,
	color string,
) (*Invite, error) {

	if timeControlMinutes <= 0 {
		return nil, ErrInvalidTimeControl
	}

	if color != "white" && color != "black" && color != "random" {
		return nil, ErrInvalidColor
	}

	code, err := generateInviteCode()
	if err != nil {
		slog.ErrorContext(ctx, "generate invite code failed", "error", err)
		return nil, ErrInternal
	}

	invite := &Invite{
		Code:               code,
		CreatorID:          userID,
		TimeControlMinutes: timeControlMinutes,
		Color:              color,
	}

	err = s.InviteRepo.SaveInvite(ctx, invite, 30*time.Minute)
	if err != nil {
		slog.ErrorContext(ctx, "save invite failed", "error", err)
		return nil, fmt.Errorf("failed to store invite: %w", err)
	}

	return invite, nil
}

func (s *Service) GetInvite(
	ctx context.Context,
	code string,
) (*Invite, error) {

	invite, err := s.InviteRepo.GetInvite(ctx, code)
	if err != nil {
		if err == redis.Nil {
			return nil, ErrInviteNotFound
		}
		slog.ErrorContext(ctx, "get invite failed", "error", err)
		return nil, ErrInternal
	}

	return invite, nil
}

func (s *Service) FindInviteCreator(ctx context.Context, creatorID int64) (*model.User, error) {
	user, err := s.UserFinder.FindUserByID(creatorID)
	if err != nil {
		slog.ErrorContext(ctx, "find invite creator failed", "error", err, "creator_id", creatorID)
		return nil, err
	}
	return user, nil
}

func (s *Service) JoinInvite(
	ctx context.Context,
	code string,
	player2ID int64,
) (string, error) {

	invite, err := s.InviteRepo.GetInvite(ctx, code)
	if err != nil {
		if err == redis.Nil {
			return "", ErrInviteNotFound
		}
		slog.ErrorContext(ctx, "get invite for join failed", "error", err)
		return "", ErrInternal
	}

	if invite.CreatorID == player2ID {
		return "", ErrSelfJoin
	}

	locked, err := s.InviteRepo.AcquireLock(ctx, code, player2ID, 10*time.Second)
	if err != nil {
		slog.ErrorContext(ctx, "acquire invite lock failed", "error", err)
		return "", ErrInternal
	}

	if !locked {
		return "", ErrAlreadyJoining
	}

	defer func() {
		if releaseErr := s.InviteRepo.ReleaseLock(ctx, code); releaseErr != nil {
			slog.ErrorContext(ctx, "release invite lock failed", "error", releaseErr)
		}
	}()

	invite, err = s.InviteRepo.GetInvite(ctx, code)
	if err != nil {
		if err == redis.Nil {
			return "", ErrInviteUsed
		}
		slog.ErrorContext(ctx, "re-fetch invite failed", "error", err)
		return "", ErrInternal
	}

	var whitePlayerID int64
	var blackPlayerID int64

	switch invite.Color {
	case "white":
		whitePlayerID = invite.CreatorID
		blackPlayerID = player2ID
	case "black":
		blackPlayerID = invite.CreatorID
		whitePlayerID = player2ID
	case "random":
		random := make([]byte, 1)
		_, err := rand.Read(random)
		if err != nil {
			slog.ErrorContext(ctx, "generate random color failed", "error", err)
			return "", ErrInternal
		}
		if random[0]%2 == 0 {
			whitePlayerID = invite.CreatorID
			blackPlayerID = player2ID
		} else {
			whitePlayerID = player2ID
			blackPlayerID = invite.CreatorID
		}
	}

	gameID, err := s.GameRepository.CreateGame(
		ctx,
		whitePlayerID,
		blackPlayerID,
		invite.TimeControlMinutes,
	)

	if err != nil {
		slog.ErrorContext(ctx, "create game from invite failed", "error", err)
		return "", fmt.Errorf("failed to create game: %w", err)
	}

	err = s.InviteRepo.DeleteInvite(ctx, code)
	if err != nil {
		slog.ErrorContext(ctx, "delete invite after join failed", "error", err)
		return "", fmt.Errorf("game created but failed to remove invite: %w", err)
	}

	slog.InfoContext(ctx, "invite joined successfully",
		"code", code,
		"game_id", gameID,
		"player2_id", player2ID,
	)

	return gameID, nil
}
