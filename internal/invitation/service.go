package invitation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/here-arjun-1/Caisaara-backend/internal/auth/repository"
	"github.com/here-arjun-1/Caisaara-backend/internal/game"
	"github.com/redis/go-redis/v9"
)

type Service struct {
	Redis          *redis.Client
	GameRepository *game.Repository
	UserRepository *repository.UserRepository
}

func NewService(
	redisClient *redis.Client,
	gameRepository *game.Repository,
	userRepository *repository.UserRepository,
) *Service {

	return &Service{
		Redis:          redisClient,
		GameRepository: gameRepository,
		UserRepository: userRepository,
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
		return nil, errors.New("invalid time control")
	}

	if color != "white" &&
		color != "black" &&
		color != "random" {

		return nil, errors.New("invalid color")
	}

	code, err := generateInviteCode()
	if err != nil {
		return nil, err
	}

	invite := &Invite{
		Code:               code,
		CreatorID:          userID,
		TimeControlMinutes: timeControlMinutes,
		Color:              color,
	}

	data, err := json.Marshal(invite)
	if err != nil {
		return nil, err
	}

	key := "invite:" + code

	err = s.Redis.Set(
		ctx,
		key,
		data,
		30*time.Minute,
	).Err()

	if err != nil {
		return nil, fmt.Errorf(
			"failed to store invite: %w",
			err,
		)
	}

	return invite, nil
}

func (s *Service) GetInvite(
	ctx context.Context,
	code string,
) (*Invite, error) {

	key := "invite:" + code

	data, err := s.Redis.Get(
		ctx,
		key,
	).Bytes()

	if err != nil {

		if errors.Is(err, redis.Nil) {
			return nil, errors.New(
				"invite not found or expired",
			)
		}

		return nil, err
	}

	var invite Invite

	err = json.Unmarshal(data, &invite)
	if err != nil {
		return nil, err
	}

	return &invite, nil
}

func (s *Service) JoinInvite(
	ctx context.Context,
	code string,
	player2ID int64,
) (string, error) {

	key := "invite:" + code

	data, err := s.Redis.Get(
		ctx,
		key,
	).Bytes()

	if err != nil {

		if errors.Is(err, redis.Nil) {
			return "", errors.New(
				"invite not found or expired",
			)
		}

		return "", err
	}

	var invite Invite

	err = json.Unmarshal(data, &invite)
	if err != nil {
		return "", err
	}

	if invite.CreatorID == player2ID {
		return "", errors.New(
			"you cannot join your own game",
		)
	}

	lockKey := "invite:lock:" + code

	locked, err := s.Redis.SetNX(
		ctx,
		lockKey,
		player2ID,
		10*time.Second,
	).Result()

	if err != nil {
		return "", err
	}

	if !locked {
		return "", errors.New(
			"someone is already joining this game",
		)
	}

	defer s.Redis.Del(ctx, lockKey)

	data, err = s.Redis.Get(
		ctx,
		key,
	).Bytes()

	if err != nil {

		if errors.Is(err, redis.Nil) {
			return "", errors.New(
				"invite already used or expired",
			)
		}

		return "", err
	}

	err = json.Unmarshal(data, &invite)
	if err != nil {
		return "", err
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
			return "", err
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
		return "", fmt.Errorf(
			"failed to create game: %w",
			err,
		)
	}

	err = s.Redis.Del(
		ctx,
		key,
	).Err()

	if err != nil {
		return "", fmt.Errorf(
			"game created but failed to remove invite: %w",
			err,
		)
	}

	return gameID, nil
}
