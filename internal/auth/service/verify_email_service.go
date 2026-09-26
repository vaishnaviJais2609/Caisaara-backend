package service

import (
	"errors"

	"github.com/here-arjun-1/Caisaara-backend/internal/auth/repository"
)

type VerifyEmailService struct {
	UserRepository *repository.UserRepository
}

func NewVerifyEmailService(
	userRepository *repository.UserRepository,
) *VerifyEmailService {
	return &VerifyEmailService{
		UserRepository: userRepository,
	}
}

func (s *VerifyEmailService) VerifyEmail(token string) error {

	if token == "" {
		return errors.New("verification token is required")
	}

	err := s.UserRepository.VerifyEmail(token)

	if err != nil {
		return errors.New("invalid or expired verification token")
	}

	return nil
}
