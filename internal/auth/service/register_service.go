package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/here-arjun-1/Caisaara-backend/internal/auth/dto"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/model"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/token"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/worker"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCode = errors.New("invalid or expired verification code")
)

type pendingUser struct {
	Req       dto.RegisterData
	CodeHash  string
	Attempts  int
	ExpiresAt time.Time
}

type RegisterService struct {
	DB                *pgxpool.Pool
	UserRepository    UserRepository
	SessionRepository SessionRepository
	RedisClient       *redis.Client
	TaskDistributor   *asynq.Client
	JWTSecret         string
}

func NewRegisterService(
	db *pgxpool.Pool,
	userRepository UserRepository,
	sessionRepository SessionRepository,
	redisClient *redis.Client,
	taskDistributor *asynq.Client,
	jwtSecret string,
) *RegisterService {

	return &RegisterService{
		DB:                db,
		UserRepository:    userRepository,
		SessionRepository: sessionRepository,
		RedisClient:       redisClient,
		TaskDistributor:   taskDistributor,
		JWTSecret:         jwtSecret,
	}
}

func (s *RegisterService) Register(ctx context.Context, req dto.RegisterData) error {
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))

	_, err := s.UserRepository.FindUserByEmail(ctx, req.Email)
	if err == nil {
		return ErrEmailTaken
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("find user by email failed", "error", err)
		return ErrInternal
	}

	_, err = s.UserRepository.FindUserByUsername(ctx, req.Username)
	if err == nil {
		return ErrUsernameTaken
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("find user by username failed", "error", err)
		return ErrInternal
	}

	code, err := generateOTP()
	if err != nil {
		slog.Error("generate otp failed", "error", err)
		return ErrInternal
	}

	codeHash := hashOTP(code)

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("hash password failed", "error", err)
		return ErrInternal
	}
	req.Password = string(hashedPassword)

	pUser := pendingUser{
		Req:       req,
		CodeHash:  codeHash,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}
	pUserJSON, _ := json.Marshal(pUser)
	s.RedisClient.Set(ctx, "register:"+req.Email, pUserJSON, 15*time.Minute)
	s.RedisClient.Set(ctx, "raw_otp:"+req.Email, code, 15*time.Minute)
	slog.Info("generated registration OTP", "email", req.Email, "otp", code)

	task, err := worker.NewEmailRegistrationTask(req.Email, code)
	if err != nil {
		slog.Error("could not create email task", "error", err)
	} else {
		info, err := s.TaskDistributor.EnqueueContext(ctx, task)
		if err != nil {
			slog.Error("could not enqueue email task", "error", err)
		} else {
			slog.Info("enqueued email task", "id", info.ID, "queue", info.Queue)
		}
	}

	return nil
}

func (s *RegisterService) VerifyRegistration(ctx context.Context, req dto.VerifyRegistrationData) (string, string, error) {
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	key := "register:" + req.Email

	var pUser pendingUser
	var returnErr error

	err := s.RedisClient.Watch(ctx, func(tx *redis.Tx) error {
		val, err := tx.Get(ctx, key).Result()
		if err != nil {
			returnErr = ErrInvalidCode
			return nil
		}

		if err := json.Unmarshal([]byte(val), &pUser); err != nil {
			returnErr = ErrInvalidCode
			return nil
		}

		if time.Now().After(pUser.ExpiresAt) {
			returnErr = ErrInvalidCode
			return nil
		}

		pUser.Attempts++
		if pUser.Attempts > maxOTPAttempts {
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Del(ctx, key)
				return nil
			})
			returnErr = errors.New("too many attempts, please request a new code")
			return err
		}

		newData, _ := json.Marshal(pUser)
		ttl := tx.TTL(ctx, key).Val()

		if !verifyOTP(pUser.CodeHash, req.Code) {
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, newData, ttl)
				return nil
			})
			returnErr = ErrInvalidCode
			return err
		}

		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Del(ctx, key)
			return nil
		})
		return err
	}, key)

	if err != nil {
		if errors.Is(err, redis.TxFailedErr) {
			return "", "", errors.New("concurrent request, please try again")
		}
		return "", "", ErrInternal
	}
	if returnErr != nil {
		return "", "", returnErr
	}

	userReq := pUser.Req

	user := &model.User{
		Username: userReq.Username,
		Email:    userReq.Email,
		Password: userReq.Password,
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		slog.Error("begin tx failed", "error", err)
		return "", "", ErrInternal
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	err = s.UserRepository.CreateUserTx(ctx, tx, user)

	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case "users_username_key":
				return "", "", ErrUsernameTaken
			case "users_email_key":
				return "", "", ErrEmailTaken
			}
		}

		slog.Error("create user failed", "error", err)

		return "", "", ErrInternal
	}

	accessToken, refreshToken, err := createSessionTokensTx(ctx, tx, s.SessionRepository, s.JWTSecret, user.ID, user.PasswordVersion)
	if err != nil {
		return "", "", err
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Error("commit tx failed", "error", err)
		return "", "", ErrInternal
	}

	return accessToken, refreshToken, nil
}

func (s *RegisterService) GuestLogin(ctx context.Context) (string, string, error) {
	guestID, err := token.GenerateGuestID()
	if err != nil {
		slog.Error("generate guest id failed", "error", err)
		return "", "", ErrInternal
	}

	guestToken, err := token.GenerateGuestToken(s.JWTSecret, guestID)
	if err != nil {
		slog.Error("generate guest token failed", "error", err)
		return "", "", ErrInternal
	}

	guestData := map[string]interface{}{
		"guest_id":   guestID,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
	guestJSON, err := json.Marshal(guestData)
	if err != nil {
		slog.Error("marshal guest data failed", "error", err)
		return "", "", ErrInternal
	}

	if s.RedisClient != nil {
		err = s.RedisClient.Set(ctx, "guest:"+guestID, guestJSON, token.GuestTokenTTL).Err()
		if err != nil {
			slog.Error("redis store guest data failed", "error", err)
			return "", "", ErrInternal
		}
	}

	return guestID, guestToken, nil
}

func hashOTP(code string) string {
	hash, _ := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	return string(hash)
}

func verifyOTP(hashedCode, rawCode string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashedCode), []byte(rawCode)) == nil
}
