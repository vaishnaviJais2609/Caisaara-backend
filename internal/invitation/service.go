package invitation

import (
	"crypto/rand"
	"encoding/hex"

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
