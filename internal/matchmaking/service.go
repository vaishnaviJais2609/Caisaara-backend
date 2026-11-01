package matchmaking

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/here-arjun-1/Caisaara-backend/internal/game"
	"github.com/here-arjun-1/Caisaara-backend/internal/tilt"
)

type TimeControl struct {
	Minutes   int `json:"time_control_minutes"`
	Increment int `json:"increment_seconds"`
}

var AllowedTimeControls = []TimeControl{
	{Minutes: 1, Increment: 0},
	{Minutes: 1, Increment: 1},
	{Minutes: 2, Increment: 1},
	{Minutes: 3, Increment: 0},
	{Minutes: 3, Increment: 2},
	{Minutes: 5, Increment: 0},
	{Minutes: 5, Increment: 2},
	{Minutes: 5, Increment: 5},
	{Minutes: 10, Increment: 0},
	{Minutes: 10, Increment: 5},
	{Minutes: 15, Increment: 10},
	{Minutes: 20, Increment: 0},
	{Minutes: 30, Increment: 0},
	{Minutes: 60, Increment: 0},
	{Minutes: 1440, Increment: 0},
	{Minutes: 2880, Increment: 0},
	{Minutes: 4320, Increment: 0},
	{Minutes: 7200, Increment: 0},
	{Minutes: 10080, Increment: 0},
	{Minutes: 20160, Increment: 0},
}

const queueTTL = 10 * time.Minute

type RatingFinder interface {
	GetRatingForMode(ctx context.Context, userID int64, mode string) (int, error)
	HasPickedLevel(ctx context.Context, userID int64) (bool, error)
}

type TiltChecker interface {
	Check(ctx context.Context, playerID int64) (*tilt.Status, error)
}

type BattleRatingFinder interface {
	GetBattleRating(ctx context.Context, userID int64) (int, error)
}

type MatchmakingService interface {
	Join(ctx context.Context, userID int64, timeControlMinutes int, incrementSeconds int, rated bool, ignoreTilt bool) (*tilt.Status, error)
	Cancel(ctx context.Context, userID int64) error
	GetStatus(ctx context.Context, userID int64) (*Status, error)
	JoinBattle(ctx context.Context, userID int64) error
	CancelBattle(ctx context.Context, userID int64) error
	GetBattleStatus(ctx context.Context, userID int64) (*Status, error)
	SetBattleRatingFinder(finder BattleRatingFinder)
}

type Service struct {
	QueueRepo    QueueRepository
	RatingFinder RatingFinder
	TiltChecker        TiltChecker
	BattleRatingFinder BattleRatingFinder
}

func NewService(queueRepo QueueRepository, ratingFinder RatingFinder, tiltChecker TiltChecker) *Service {
	return &Service{
		QueueRepo:    queueRepo,
		RatingFinder: ratingFinder,
		TiltChecker:  tiltChecker,
	}
}

func isAllowedTimeControl(timeControlMinutes int, incrementSeconds int) bool {
	for _, tc := range AllowedTimeControls {
		if tc.Minutes == timeControlMinutes && tc.Increment == incrementSeconds {
			return true
		}
	}

	if timeControlMinutes >= 0 && timeControlMinutes <= 120 && incrementSeconds >= 0 && incrementSeconds <= 60 && (timeControlMinutes > 0 || incrementSeconds > 0) {
		return true
	}

	return false
}

func (s *Service) Join(
	ctx context.Context,
	userID int64,
	timeControlMinutes int,
	incrementSeconds int,
	rated bool,
	ignoreTilt bool,
) (*tilt.Status, error) {

	if !isAllowedTimeControl(timeControlMinutes, incrementSeconds) {
		return nil, ErrInvalidTimeControl
	}

	if rated {
		picked, err := s.RatingFinder.HasPickedLevel(ctx, userID)
		if err != nil {
			slog.ErrorContext(ctx, "check picked level failed", "error", err, "user_id", userID)
			return nil, ErrInternal
		}
		if !picked {
			return nil, ErrLevelNotPicked
		}
	}

	if rated && !ignoreTilt {
		tiltStatus, err := s.TiltChecker.Check(ctx, userID)
		if err != nil {
			slog.ErrorContext(ctx, "tilt check failed", "error", err, "user_id", userID)
		} else if tiltStatus != nil && tiltStatus.Tilted {
			return tiltStatus, ErrTilted
		}
	}

	mode, _, _, _ := game.ResolveTimeControlMode(timeControlMinutes, incrementSeconds)

	rating, err := s.RatingFinder.GetRatingForMode(ctx, userID, mode)
	if err != nil {
		slog.ErrorContext(ctx, "find rating for matchmaking failed", "error", err, "user_id", userID, "mode", mode)
		return nil, ErrInternal
	}

	_, err = s.QueueRepo.RemoveFromQueue(ctx, userID)
	if err != nil && !errors.Is(err, ErrNotInQueue) {
		slog.ErrorContext(ctx, "remove old queue entry failed", "error", err, "user_id", userID)
		return nil, ErrInternal
	}

	entry := &QueueEntry{
		UserID:             userID,
		Rating:             rating,
		TimeControlMinutes: timeControlMinutes,
		IncrementSeconds:   incrementSeconds,
		Rated:              rated,
		JoinedAt:           time.Now(),
	}

	err = s.QueueRepo.AddToQueue(ctx, entry, queueTTL)
	if err != nil {
		if errors.Is(err, ErrAlreadyInQueue) {
			return nil, ErrAlreadyInQueue
		}
		slog.ErrorContext(ctx, "add to queue failed", "error", err, "user_id", userID)
		return nil, ErrInternal
	}

	return nil, nil
}

func (s *Service) Cancel(ctx context.Context, userID int64) error {
	_, err := s.QueueRepo.RemoveFromQueue(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotInQueue) {
			return ErrNotInQueue
		}
		slog.ErrorContext(ctx, "remove from queue failed", "error", err, "user_id", userID)
		return ErrInternal
	}

	return nil
}

func (s *Service) GetStatus(ctx context.Context, userID int64) (*Status, error) {
	_, err := s.QueueRepo.GetEntry(ctx, userID)
	if err == nil {
		return &Status{State: StatusSearching}, nil
	}
	if !errors.Is(err, ErrNotInQueue) {
		slog.ErrorContext(ctx, "get queue entry failed", "error", err, "user_id", userID)
		return nil, ErrInternal
	}

	gameID, err := s.QueueRepo.GetMatch(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "get match failed", "error", err, "user_id", userID)
		return nil, ErrInternal
	}

	if gameID != "" {
		return &Status{State: StatusMatched, GameID: gameID}, nil
	}

	return &Status{State: StatusIdle}, nil
}


func (s *Service) SetBattleRatingFinder(finder BattleRatingFinder) {
	s.BattleRatingFinder = finder
}

func (s *Service) JoinBattle(ctx context.Context, userID int64) error {
	rating := 1200
	if s.BattleRatingFinder != nil {
		if r, err := s.BattleRatingFinder.GetBattleRating(ctx, userID); err == nil && r > 0 {
			rating = r
		}
	}
	return s.QueueRepo.AddToBattleQueue(ctx, userID, rating, queueTTL)
}

func (s *Service) CancelBattle(ctx context.Context, userID int64) error {
	_, err := s.QueueRepo.RemoveFromBattleQueue(ctx, userID)
	return err
}

func (s *Service) GetBattleStatus(ctx context.Context, userID int64) (*Status, error) {
	matchID, err := s.QueueRepo.GetMatch(ctx, userID)
	if err == nil && matchID != "" {
		return &Status{
			State:  StatusMatched,
			GameID: matchID,
		}, nil
	}

	queue, err := s.QueueRepo.GetBattleQueue(ctx)
	if err == nil {
		for _, entry := range queue {
			if entry.UserID == userID {
				return &Status{
					State: StatusSearching,
				}, nil
			}
		}
	}

	return &Status{
		State: StatusIdle,
	}, nil
}
