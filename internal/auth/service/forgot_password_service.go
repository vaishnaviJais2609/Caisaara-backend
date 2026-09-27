package service

import (
	"errors"
	"log"
	"strings"
	"time"

	"github.com/here-arjun-1/Caisaara-backend/internal/auth/email"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/model"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/repository"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/token"
	"github.com/jackc/pgx/v5"
)

type ForgotPasswordService struct {
	UserRepository          *repository.UserRepository
	PasswordResetRepository *repository.PasswordResetRepository
}

func NewForgotPasswordService(
	userRepository *repository.UserRepository,
	passwordResetRepository *repository.PasswordResetRepository,
) *ForgotPasswordService {

	return &ForgotPasswordService{
		UserRepository:          userRepository,
		PasswordResetRepository: passwordResetRepository,
	}
}

func (s *ForgotPasswordService) ForgotPassword(
	emailAddress string,
) error {

	emailAddress = strings.ToLower(strings.TrimSpace(emailAddress))

	user, err := s.UserRepository.FindUserByEmail(emailAddress)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}

	if err != nil {
		log.Printf("find user by email failed: %v", err)
		return ErrInternal
	}

	code, err := token.GeneratePasswordResetCode()
	if err != nil {
		log.Printf("generate reset code failed: %v", err)
		return ErrInternal
	}

	codeHash := token.HashPasswordResetCode(code)

	reset := &model.PasswordResetToken{
		UserID:    user.ID,
		CodeHash:  codeHash,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}

	err = s.PasswordResetRepository.Create(reset)
	if err != nil {
		log.Printf("create password reset failed: %v", err)
		return ErrInternal
	}

	err = email.SendPasswordResetCode(
		user.Email,
		code,
	)

	if err != nil {
		log.Printf("send password reset email failed: %v", err)
	}

	return nil
}
