package service

import (
	"errors"
	"time"

	"github.com/here-arjun-1/Caisaara-backend/internal/auth/dto"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/email"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/model"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/repository"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/token"
	"golang.org/x/crypto/bcrypt"
)

type RegisterService struct {
	UserRepository *repository.UserRepository
}

func NewRegisterService(userRepository *repository.UserRepository) *RegisterService {
	return &RegisterService{
		UserRepository: userRepository,
	}
}

func (s *RegisterService) Register(req dto.RegisterData) error {

	if req.Username == "" {
		return errors.New("username is required")
	}

	if req.Email == "" {
		return errors.New("email is required")
	}

	if req.Password == "" {
		return errors.New("password is required")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword(
		[]byte(req.Password),
		bcrypt.DefaultCost,
	)

	if err != nil {
		return errors.New("failed to process password")
	}

	verificationToken, err := token.GenerateEmailVerificationToken()
	if err != nil {
		return errors.New("failed to generate verification token")
	}

	user := &model.User{
		Username:                   req.Username,
		Email:                      req.Email,
		Password:                   string(hashedPassword),
		EmailVerified:              false,
		EmailVerificationToken:     verificationToken,
		EmailVerificationExpiresAt: time.Now().Add(15 * time.Minute),
	}

	err = s.UserRepository.CreateUser(user)

	if err != nil {
		return errors.New("failed to create user")
	}

	verificationLink := "http://localhost:8050/verify-email?token=" + verificationToken

	err = email.SendVerificationEmail(
		req.Email,
		verificationLink,
	)

	if err != nil {
		return errors.New("failed to send verification email")
	}

	return nil
}
