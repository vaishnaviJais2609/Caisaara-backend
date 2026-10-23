package puzzle

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidUCIMove        = errors.New("invalid UCI move")
	ErrInvalidMode           = errors.New("invalid mode")
	ErrInvalidRatingRange    = errors.New("invalid rating range")
	ErrPuzzleNotFound        = errors.New("puzzle not found")
	ErrNoMatchingPuzzle      = errors.New("no puzzle matches the filters")
	ErrRushSessionNotFound   = errors.New("rush session not found")
	ErrPuzzleAlreadySolved   = errors.New("puzzle already solved today")
	ErrPuzzleAlreadyFailed   = errors.New("no attempts remaining")
	ErrHintAlreadyUsed       = errors.New("hint already used")
	ErrRushSessionEnded      = errors.New("rush session already ended")
	ErrPuzzleSessionMismatch = errors.New("puzzle id does not match session")
	ErrPuzzleNotFinished     = errors.New("puzzle has not been completed by user yet")
)

type PuzzleService interface {
	GetDailyPuzzle(ctx context.Context, userID int64, tz ...string) (*DailyPuzzleResponse, error)
	SubmitAttempt(ctx context.Context, userID int64, req SubmitAttemptRequest, tz ...string) (*SubmitAttemptResponse, error)
	GetHint(ctx context.Context, userID int64, req HintRequest, tz ...string) (*HintResponse, error)
	GetSolution(ctx context.Context, userID int64, req SolutionRequest, tz ...string) (*SolutionResponse, error)
	GetNextPuzzle(ctx context.Context, userID int64, theme string) (*NextPuzzleResponse, error)
	GetUserStats(ctx context.Context, userID int64) (*PuzzleStatsResponse, error)
	GetUserHistory(ctx context.Context, userID int64, limit, offset int) (*PuzzleHistoryResponse, error)
	GetPuzzleByID(ctx context.Context, userID int64, puzzleID string) (*PuzzleDetailsResponse, error)
	StartRush(ctx context.Context, userID int64, req StartRushRequest) (*StartRushResponse, error)
	SubmitRushMove(ctx context.Context, userID int64, req SubmitRushMoveRequest) (*SubmitRushMoveResponse, error)
	EndRush(ctx context.Context, userID int64, req EndRushRequest) (*RushSummary, error)
	GetRushLeaderboard(ctx context.Context, mode string, limit int) (*RushLeaderboardResponse, error)
	GetUserRushStats(ctx context.Context, userID int64) (*UserRushStatsResponse, error)
	GetUserRushHistory(ctx context.Context, userID int64, mode string, limit, offset int) (*RushHistoryResponse, error)
	GetThemes(ctx context.Context) (*ThemesResponse, error)
	GetCustomPuzzle(ctx context.Context, userID int64, filter CustomPuzzleFilter) (*CustomPuzzleResponse, error)
	SubmitCustomAttempt(ctx context.Context, userID int64, req SubmitCustomAttemptRequest) (*SubmitCustomAttemptResponse, error)
}

const lockStripes = 256

type userLockStripes struct {
	locks [lockStripes]sync.Mutex
}

func (l *userLockStripes) lock(userID int64) {
	idx := int(userID & (lockStripes - 1))
	l.locks[idx].Lock()
}

func (l *userLockStripes) unlock(userID int64) {
	idx := int(userID & (lockStripes - 1))
	l.locks[idx].Unlock()
}

type stringLockStripes struct {
	locks [lockStripes]sync.Mutex
}

func (l *stringLockStripes) lock(key string) {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= 1099511628211
	}
	idx := h % lockStripes
	l.locks[idx].Lock()
}

func (l *stringLockStripes) unlock(key string) {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= 1099511628211
	}
	idx := h % lockStripes
	l.locks[idx].Unlock()
}

type Service struct {
	repo      PuzzleRepository
	userLocks userLockStripes
	rushLocks stringLockStripes
}

func NewService(repo PuzzleRepository) PuzzleService {
	return &Service{
		repo: repo,
	}
}

func isValidUCIMove(m string) bool {
	m = strings.TrimSpace(m)
	if strings.Contains(m, " ") {
		parts := strings.Fields(m)
		if len(parts) == 0 {
			return false
		}
		for _, p := range parts {
			if !isValidSingleUCIMove(p) {
				return false
			}
		}
		return true
	}
	return isValidSingleUCIMove(m)
}

func isValidSingleUCIMove(m string) bool {
	if len(m) != 4 && len(m) != 5 {
		return false
	}
	if m[0] < 'a' || m[0] > 'h' || m[1] < '1' || m[1] > '8' {
		return false
	}
	if m[2] < 'a' || m[2] > 'h' || m[3] < '1' || m[3] > '8' {
		return false
	}
	if len(m) == 5 {
		p := strings.ToLower(string(m[4]))
		if p != "q" && p != "r" && p != "b" && p != "n" {
			return false
		}
	}
	return true
}

func getTodayAndYesterday(tz string) (string, string) {
	loc := time.UTC
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	now := time.Now().In(loc)
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	return today, yesterday
}

func (s *Service) GetDailyPuzzle(ctx context.Context, userID int64, tz ...string) (*DailyPuzzleResponse, error) {
	s.userLocks.lock(userID)
	defer s.userLocks.unlock(userID)

	timeZone := ""
	if len(tz) > 0 {
		timeZone = tz[0]
	}
	today, yesterday := getTodayAndYesterday(timeZone)

	ratingProfile, err := s.repo.GetOrCreateUserRating(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user rating: %w", err)
	}

	if ratingProfile.LastPlayedDate != "" && ratingProfile.LastPlayedDate != today && ratingProfile.LastPlayedDate != yesterday {
		ratingProfile.CurrentStreak = 0
		_ = s.repo.UpdateUserRating(ctx, ratingProfile)
	}

	session, err := s.repo.GetDailySession(ctx, userID, today)
	if err != nil {
		return nil, fmt.Errorf("get daily session: %w", err)
	}

	var puzzleRecord *Puzzle
	if session != nil {
		puzzleRecord, err = s.repo.FindPuzzleByID(ctx, session.PuzzleID)
		if err != nil {
			return nil, fmt.Errorf("find session puzzle: %w", err)
		}
	} else {
		targetRating := ratingProfile.Rating + (ratingProfile.CurrentStreak * 10)
		if targetRating > 2800 {
			targetRating = 2800
		}

		puzzleRecord, err = s.repo.FindMatchingPuzzle(ctx, userID, targetRating-60, targetRating+60, "")
		if err != nil {
			return nil, fmt.Errorf("find daily puzzle: %w", err)
		}

		session = &DailyPuzzleSession{
			UserID:         userID,
			PuzzleID:       puzzleRecord.ID,
			Date:           today,
			AttemptsLeft:   3,
			HintUsed:       false,
			SolutionViewed: false,
			Solved:         false,
			RatingEarned:   0,
			AttemptedMoves: []string{},
		}

		if err := s.repo.CreateDailySession(ctx, session); err != nil {
			return nil, fmt.Errorf("create daily session: %w", err)
		}
	}

	moves := strings.Fields(puzzleRecord.Moves)
	initialMove := ""
	totalMoves := 0
	playerMovesCount := 0
	if len(moves) > 0 {
		initialMove = moves[0]
		if len(moves) > 1 {
			totalMoves = len(moves) - 1
			playerMovesCount = len(moves) / 2
		}
	}

	return &DailyPuzzleResponse{
		PuzzleID:         puzzleRecord.ID,
		FEN:              puzzleRecord.FEN,
		InitialMove:      initialMove,
		Rating:           puzzleRecord.Rating,
		Themes:           puzzleRecord.Themes,
		AttemptsLeft:     session.AttemptsLeft,
		HintAvailable:    !session.HintUsed,
		SolutionViewed:   session.SolutionViewed,
		Solved:           session.Solved,
		CurrentStreak:    ratingProfile.CurrentStreak,
		PlayerRating:     ratingProfile.Rating,
		AttemptedMoves:   session.AttemptedMoves,
		TotalMoves:       totalMoves,
		PlayerMovesCount: playerMovesCount,
	}, nil
}

func (s *Service) SubmitAttempt(ctx context.Context, userID int64, req SubmitAttemptRequest, tz ...string) (*SubmitAttemptResponse, error) {
	s.userLocks.lock(userID)
	defer s.userLocks.unlock(userID)

	req.Move = strings.TrimSpace(req.Move)
	req.PuzzleID = strings.TrimSpace(req.PuzzleID)

	if !isValidUCIMove(req.Move) {
		return nil, ErrInvalidUCIMove
	}

	timeZone := ""
	if len(tz) > 0 {
		timeZone = tz[0]
	}
	today, _ := getTodayAndYesterday(timeZone)

	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	session, err := s.repo.GetDailySessionForUpdate(ctx, tx, userID, today)
	if err != nil {
		return nil, fmt.Errorf("get daily session: %w", err)
	}

	if session == nil {
		return nil, ErrPuzzleNotFound
	}
	if session.PuzzleID != req.PuzzleID {
		return nil, ErrPuzzleSessionMismatch
	}

	ratingProfile, err := s.repo.GetOrCreateUserRatingForUpdate(ctx, tx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user rating: %w", err)
	}

	if session.Solved {
		return nil, ErrPuzzleAlreadySolved
	}

	if session.AttemptsLeft <= 0 {
		return nil, ErrPuzzleAlreadyFailed
	}

	puzzleRecord, err := s.repo.FindPuzzleByID(ctx, req.PuzzleID)
	if err != nil {
		return nil, fmt.Errorf("find puzzle: %w", err)
	}

	moves := strings.Fields(puzzleRecord.Moves)
	if len(moves) < 2 {
		return nil, errors.New("invalid puzzle moves sequence")
	}

	currentPly := 0
	if session != nil && len(session.AttemptedMoves) < len(moves)-1 {
		currentPly = len(session.AttemptedMoves)
	}
	expectedIndex := 1 + currentPly
	if expectedIndex >= len(moves) {
		expectedIndex = 1
		if session != nil {
			session.AttemptedMoves = []string{}
		}
	}

	playedMove := strings.TrimSpace(req.Move)
	correctMove := moves[expectedIndex]
	isCorrect := strings.EqualFold(playedMove, correctMove)
	isFullySolved := false
	nextMove := ""

	fullExpected := strings.Join(moves[1:], " ")
	var playerMoves []string
	for i := 1; i < len(moves); i += 2 {
		playerMoves = append(playerMoves, moves[i])
	}
	playerExpected := strings.Join(playerMoves, " ")

	if !isCorrect && strings.Contains(playedMove, " ") {
		if strings.EqualFold(playedMove, fullExpected) || strings.EqualFold(playedMove, playerExpected) {
			isCorrect = true
			isFullySolved = true
			nextMove = ""
			if session != nil {
				session.AttemptedMoves = moves[1:]
			}
		} else if expectedIndex > 1 {
			remainingExpected := strings.Join(moves[expectedIndex:], " ")
			if strings.EqualFold(playedMove, remainingExpected) {
				isCorrect = true
				isFullySolved = true
				nextMove = ""
				if session != nil {
					session.AttemptedMoves = moves[1:]
				}
			}
		}
	} else if isCorrect {
		if expectedIndex+1 < len(moves) {
			nextMove = moves[expectedIndex+1]
			isFullySolved = false
			if session != nil {
				session.AttemptedMoves = append(session.AttemptedMoves, playedMove, nextMove)
			}
		} else {
			isFullySolved = true
			nextMove = ""
			if session != nil {
				session.AttemptedMoves = append(session.AttemptedMoves, playedMove)
			}
		}
	}

	recordedMoves := []string{playedMove}
	if session != nil {
		recordedMoves = session.AttemptedMoves
	}

	attemptsLeft := 3
	if session != nil {
		attemptsLeft = session.AttemptsLeft
	}

	if !isCorrect {
		if session != nil {
			session.AttemptsLeft--
			attemptsLeft = session.AttemptsLeft
			recordedMoves = append([]string{}, session.AttemptedMoves...)
			recordedMoves = append(recordedMoves, playedMove)
			if err := s.repo.UpdateDailySessionTx(ctx, tx, session); err != nil {
				return nil, fmt.Errorf("update daily session: %w", err)
			}
		}

		if attemptsLeft <= 0 {
			ratingChange := calculateRatingChange(ratingProfile.Rating, puzzleRecord.Rating, false, attemptsLeft)
			beforeRating := ratingProfile.Rating
			ratingProfile.Rating += ratingChange
			if ratingProfile.Rating < 100 {
				ratingProfile.Rating = 100
			}
			ratingProfile.PuzzlesFailed++
			ratingProfile.CurrentStreak = 0
			ratingProfile.LastPlayedDate = today

			if err := s.repo.UpdateUserRatingTx(ctx, tx, ratingProfile); err != nil {
				return nil, fmt.Errorf("update user rating: %w", err)
			}

			attempt := &UserPuzzleAttempt{
				UserID:           userID,
				PuzzleID:         puzzleRecord.ID,
				Solved:           false,
				UserRatingBefore: beforeRating,
				UserRatingAfter:  ratingProfile.Rating,
				TimeTakenMs:      req.TimeTakenMs,
				AttemptedMoves:   recordedMoves,
			}
			if err := s.repo.RecordAttemptTx(ctx, tx, attempt); err != nil {
				return nil, fmt.Errorf("record attempt: %w", err)
			}

			if err := tx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("commit tx: %w", err)
			}

			return &SubmitAttemptResponse{
				Correct:          false,
				Solved:           false,
				AttemptsLeft:     0,
				RatingChange:     ratingChange,
				UserRatingBefore: beforeRating,
				UserRatingAfter:  ratingProfile.Rating,
				CurrentStreak:    ratingProfile.CurrentStreak,
				AttemptedMoves:   recordedMoves,
				Message:          "No attempts remaining. You can view the solution.",
			}, nil
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}

		return &SubmitAttemptResponse{
			Correct:          false,
			Solved:           false,
			AttemptsLeft:     attemptsLeft,
			RatingChange:     0,
			UserRatingBefore: ratingProfile.Rating,
			UserRatingAfter:  ratingProfile.Rating,
			CurrentStreak:    ratingProfile.CurrentStreak,
			AttemptedMoves:   recordedMoves,
			Message:          fmt.Sprintf("Incorrect move. %d attempts remaining.", attemptsLeft),
		}, nil
	}

	if !isFullySolved {
		if err := s.repo.UpdateDailySessionTx(ctx, tx, session); err != nil {
			return nil, fmt.Errorf("update daily session: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}

		return &SubmitAttemptResponse{
			Correct:          true,
			Solved:           false,
			NextMove:         nextMove,
			AttemptsLeft:     attemptsLeft,
			RatingChange:     0,
			UserRatingBefore: ratingProfile.Rating,
			UserRatingAfter:  ratingProfile.Rating,
			CurrentStreak:    ratingProfile.CurrentStreak,
			AttemptedMoves:   recordedMoves,
			Message:          "Correct move! Keep going...",
		}, nil
	}

	ratingChange := 0
	beforeRating := ratingProfile.Rating

	if session != nil && session.SolutionViewed {
		session.Solved = true
		session.RatingEarned = 0
		if err := s.repo.UpdateDailySessionTx(ctx, tx, session); err != nil {
			return nil, fmt.Errorf("update daily session: %w", err)
		}

		attempt := &UserPuzzleAttempt{
			UserID:           userID,
			PuzzleID:         puzzleRecord.ID,
			Solved:           true,
			UserRatingBefore: beforeRating,
			UserRatingAfter:  beforeRating,
			TimeTakenMs:      req.TimeTakenMs,
			AttemptedMoves:   recordedMoves,
		}
		if err := s.repo.RecordAttemptTx(ctx, tx, attempt); err != nil {
			return nil, fmt.Errorf("record attempt: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}

		return &SubmitAttemptResponse{
			Correct:          true,
			Solved:           true,
			NextMove:         "",
			AttemptsLeft:     attemptsLeft,
			RatingChange:     0,
			UserRatingBefore: beforeRating,
			UserRatingAfter:  beforeRating,
			CurrentStreak:    ratingProfile.CurrentStreak,
			AttemptedMoves:   recordedMoves,
			Message:          "Correct move! However, no rating points are awarded because the solution was viewed.",
		}, nil
	}

	ratingChange = calculateRatingChange(ratingProfile.Rating, puzzleRecord.Rating, true, attemptsLeft)
	ratingProfile.Rating += ratingChange
	if ratingProfile.Rating > ratingProfile.BestRating {
		ratingProfile.BestRating = ratingProfile.Rating
		now := time.Now()
		ratingProfile.BestRatingAt = now
	}
	ratingProfile.PuzzlesSolved++

	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	if ratingProfile.LastPlayedDate != "" && ratingProfile.LastPlayedDate != today && ratingProfile.LastPlayedDate != yesterday {
		ratingProfile.CurrentStreak = 0
	}

	if ratingProfile.LastPlayedDate != today {
		ratingProfile.CurrentStreak++
		if ratingProfile.CurrentStreak > ratingProfile.MaxStreak {
			ratingProfile.MaxStreak = ratingProfile.CurrentStreak
		}
		ratingProfile.LastPlayedDate = today
	}

	if err := s.repo.UpdateUserRatingTx(ctx, tx, ratingProfile); err != nil {
		return nil, fmt.Errorf("update user rating: %w", err)
	}

	if session != nil {
		session.Solved = true
		session.RatingEarned = ratingChange
		if err := s.repo.UpdateDailySessionTx(ctx, tx, session); err != nil {
			return nil, fmt.Errorf("update daily session: %w", err)
		}
	}

	attempt := &UserPuzzleAttempt{
		UserID:           userID,
		PuzzleID:         puzzleRecord.ID,
		Solved:           true,
		UserRatingBefore: beforeRating,
		UserRatingAfter:  ratingProfile.Rating,
		TimeTakenMs:      req.TimeTakenMs,
		AttemptedMoves:   recordedMoves,
	}
	if err := s.repo.RecordAttemptTx(ctx, tx, attempt); err != nil {
		return nil, fmt.Errorf("record attempt: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &SubmitAttemptResponse{
		Correct:          true,
		Solved:           true,
		NextMove:         "",
		AttemptsLeft:     attemptsLeft,
		RatingChange:     ratingChange,
		UserRatingBefore: beforeRating,
		UserRatingAfter:  ratingProfile.Rating,
		CurrentStreak:    ratingProfile.CurrentStreak,
		AttemptedMoves:   recordedMoves,
		Message:          "Puzzle solved successfully!",
	}, nil
}

func (s *Service) GetHint(ctx context.Context, userID int64, req HintRequest, tz ...string) (*HintResponse, error) {
	s.userLocks.lock(userID)
	defer s.userLocks.unlock(userID)

	req.PuzzleID = strings.TrimSpace(req.PuzzleID)

	timeZone := ""
	if len(tz) > 0 {
		timeZone = tz[0]
	}
	today, _ := getTodayAndYesterday(timeZone)

	session, err := s.repo.GetDailySession(ctx, userID, today)
	if err != nil {
		return nil, fmt.Errorf("get daily session: %w", err)
	}
	if session == nil {
		return nil, ErrPuzzleNotFound
	}
	if req.PuzzleID != "" && session.PuzzleID != req.PuzzleID {
		return nil, ErrPuzzleSessionMismatch
	}
	if session.HintUsed {
		return nil, ErrHintAlreadyUsed
	}

	puzzleRecord, err := s.repo.FindPuzzleByID(ctx, session.PuzzleID)
	if err != nil {
		return nil, fmt.Errorf("find puzzle: %w", err)
	}

	moves := strings.Fields(puzzleRecord.Moves)
	if len(moves) < 2 {
		return nil, errors.New("invalid puzzle moves sequence")
	}

	expectedIndex := 1 + len(session.AttemptedMoves)
	if expectedIndex >= len(moves) {
		expectedIndex = 1
	}

	correctMove := moves[expectedIndex]
	fromSquare := ""
	if len(correctMove) >= 2 {
		fromSquare = correctMove[:2]
	}

	if !session.HintUsed {
		_ = s.repo.SetDailyHintUsed(ctx, userID, today)
	}

	return &HintResponse{
		HintType:   "piece",
		FromSquare: fromSquare,
		Message:    fmt.Sprintf("Focus on the piece on %s!", fromSquare),
		HintUsed:   true,
	}, nil
}

func (s *Service) GetSolution(ctx context.Context, userID int64, req SolutionRequest, tz ...string) (*SolutionResponse, error) {
	s.userLocks.lock(userID)
	defer s.userLocks.unlock(userID)

	req.PuzzleID = strings.TrimSpace(req.PuzzleID)

	timeZone := ""
	if len(tz) > 0 {
		timeZone = tz[0]
	}
	today, _ := getTodayAndYesterday(timeZone)

	session, err := s.repo.GetDailySession(ctx, userID, today)
	if err != nil {
		return nil, fmt.Errorf("get daily session: %w", err)
	}
	if session == nil {
		return nil, ErrPuzzleNotFound
	}
	if req.PuzzleID != "" && session.PuzzleID != req.PuzzleID {
		return nil, ErrPuzzleSessionMismatch
	}

	puzzleRecord, err := s.repo.FindPuzzleByID(ctx, session.PuzzleID)
	if err != nil {
		return nil, fmt.Errorf("find puzzle: %w", err)
	}

	moves := strings.Fields(puzzleRecord.Moves)
	solutionMoves := []string{}
	if len(moves) > 1 {
		solutionMoves = moves[1:]
	}

	_ = s.repo.SetDailySolutionViewed(ctx, userID, today)

	return &SolutionResponse{
		Moves:          solutionMoves,
		RatingChange:   0,
		SolutionViewed: true,
		Message:        "Solution revealed. No rating increase will be awarded.",
	}, nil
}

func (s *Service) GetNextPuzzle(ctx context.Context, userID int64, theme string) (*NextPuzzleResponse, error) {
	ratingProfile, err := s.repo.GetOrCreateUserRating(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user rating: %w", err)
	}

	minRating := ratingProfile.Rating - 75
	maxRating := ratingProfile.Rating + 75

	p, err := s.repo.FindMatchingPuzzle(ctx, userID, minRating, maxRating, theme)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoMatchingPuzzle
		}
		return nil, fmt.Errorf("find matching puzzle: %w", err)
	}

	moves := strings.Fields(p.Moves)
	initialMove := ""
	totalMoves := 0
	playerMovesCount := 0
	if len(moves) > 0 {
		initialMove = moves[0]
		if len(moves) > 1 {
			totalMoves = len(moves) - 1
			playerMovesCount = len(moves) / 2
		}
	}

	return &NextPuzzleResponse{
		PuzzleID:         p.ID,
		FEN:              p.FEN,
		InitialMove:      initialMove,
		Rating:           p.Rating,
		Themes:           p.Themes,
		TotalMoves:       totalMoves,
		PlayerMovesCount: playerMovesCount,
	}, nil
}

func (s *Service) GetPuzzleByID(ctx context.Context, userID int64, puzzleID string) (*PuzzleDetailsResponse, error) {
	puzzleID = strings.TrimSpace(puzzleID)
	if puzzleID == "" {
		return nil, ErrPuzzleNotFound
	}

	finished, err := s.repo.HasUserFinishedPuzzle(ctx, userID, puzzleID)
	if err != nil {
		return nil, fmt.Errorf("check puzzle finished: %w", err)
	}
	if !finished {
		return nil, ErrPuzzleNotFinished
	}

	p, err := s.repo.FindPuzzleByID(ctx, puzzleID)
	if errors.Is(err, pgx.ErrNoRows) || p == nil {
		return nil, ErrPuzzleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find puzzle by id: %w", err)
	}

	moves := strings.Fields(p.Moves)
	initialMove := ""
	solutionMoves := []string{}
	if len(moves) > 0 {
		initialMove = moves[0]
		if len(moves) > 1 {
			solutionMoves = moves[1:]
		}
	}

	return &PuzzleDetailsResponse{
		PuzzleID:      p.ID,
		FEN:           p.FEN,
		InitialMove:   initialMove,
		Rating:        p.Rating,
		Themes:        p.Themes,
		SolutionMoves: solutionMoves,
	}, nil
}

func (s *Service) GetUserStats(ctx context.Context, userID int64) (*PuzzleStatsResponse, error) {
	return s.repo.GetUserStats(ctx, userID)
}

func (s *Service) GetUserHistory(ctx context.Context, userID int64, limit, offset int) (*PuzzleHistoryResponse, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.GetUserHistory(ctx, userID, limit, offset)
}

func calculateRatingChange(playerRating, puzzleRating int, solved bool, attemptsLeft int) int {
	diff := float64(puzzleRating - playerRating)
	expected := 1.0 / (1.0 + math.Pow(10, -diff/400.0))

	if solved {
		k := 32.0
		gain := k * (1.0 - expected)
		switch attemptsLeft {
		case 2:
			gain *= 0.8
		case 1:
			gain *= 0.6
		}
		if gain < 5 {
			gain = 5
		}
		if gain > 32 {
			gain = 32
		}
		return int(math.Round(gain))
	}

	k := 16.0
	loss := k * expected
	if loss < 5 {
		loss = 5
	}
	if loss > 20 {
		loss = 20
	}
	return -int(math.Round(loss))
}

func (s *Service) StartRush(ctx context.Context, userID int64, req StartRushRequest) (*StartRushResponse, error) {
	s.userLocks.lock(userID)
	defer s.userLocks.unlock(userID)

	if err := s.repo.CloseActiveRushSessionsForUser(ctx, userID); err != nil {
		return nil, fmt.Errorf("close active rush sessions: %w", err)
	}

	var timeLimitSec int
	switch req.Mode {
	case "3min":
		timeLimitSec = 180
	case "5min":
		timeLimitSec = 300
	case "survival":
		timeLimitSec = 0
	default:
		return nil, ErrInvalidMode
	}

	minRating, maxRating := getRushRatingWindow(0)
	puzzle, err := s.repo.FindRushPuzzle(ctx, minRating, maxRating, []string{}, req.Theme)
	if err != nil {
		return nil, fmt.Errorf("find initial rush puzzle: %w", err)
	}

	now := time.Now().UTC()
	session := &PuzzleRushSession{
		ID:              uuid.New().String(),
		UserID:          userID,
		Mode:            req.Mode,
		Theme:           req.Theme,
		Score:           0,
		Strikes:         0,
		CurrentPuzzleID: puzzle.ID,
		PassedPuzzleIDs: []string{},
		Status:          "active",
		TimeLimitSec:    timeLimitSec,
		MovesHistory:    []RushPuzzleAttempt{},
		StartedAt:       now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.repo.CreateRushSession(ctx, session); err != nil {
		return nil, fmt.Errorf("create rush session: %w", err)
	}

	moves := strings.Fields(puzzle.Moves)
	initialMove := ""
	totalMoves := 0
	playerMovesCount := 0
	if len(moves) > 0 {
		initialMove = moves[0]
		if len(moves) > 1 {
			totalMoves = len(moves) - 1
			playerMovesCount = len(moves) / 2
		}
	}

	return &StartRushResponse{
		SessionID:    session.ID,
		Mode:         session.Mode,
		Theme:        session.Theme,
		TimeLimitSec: session.TimeLimitSec,
		Score:        session.Score,
		Strikes:      session.Strikes,
		StrikesLeft:  3,
		Puzzle: RushPuzzleData{
			PuzzleID:         puzzle.ID,
			FEN:              puzzle.FEN,
			InitialMove:      initialMove,
			Rating:           puzzle.Rating,
			Themes:           puzzle.Themes,
			TotalMoves:       totalMoves,
			PlayerMovesCount: playerMovesCount,
		},
	}, nil
}

func (s *Service) SubmitRushMove(ctx context.Context, userID int64, req SubmitRushMoveRequest) (*SubmitRushMoveResponse, error) {
	req.Move = strings.TrimSpace(req.Move)
	req.PuzzleID = strings.TrimSpace(req.PuzzleID)

	if !isValidUCIMove(req.Move) {
		return nil, ErrInvalidUCIMove
	}

	s.rushLocks.lock(req.SessionID)
	defer s.rushLocks.unlock(req.SessionID)

	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	session, err := s.repo.GetRushSessionForUpdate(ctx, tx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("get rush session: %w", err)
	}
	if session == nil {
		return nil, ErrRushSessionNotFound
	}
	if session.UserID != userID {
		return nil, ErrRushSessionNotFound
	}

	if session.Status != "active" {
		return nil, ErrRushSessionEnded
	}

	if session.TimeLimitSec > 0 {
		elapsed := time.Since(session.StartedAt).Seconds()
		if elapsed > float64(session.TimeLimitSec+2) {
			session.Status = "time_expired"
			now := time.Now().UTC()
			session.EndedAt = &now
			if err := s.repo.UpdateRushSessionTx(ctx, tx, session); err != nil {
				return nil, fmt.Errorf("update rush session: %w", err)
			}

			summary, err := s.finalizeRushSessionTx(ctx, tx, session)
			if err != nil {
				return nil, fmt.Errorf("finalize rush session: %w", err)
			}

			if err := tx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("commit tx: %w", err)
			}

			return &SubmitRushMoveResponse{
				Correct:     false,
				Score:       session.Score,
				Strikes:     session.Strikes,
				StrikesLeft: 0,
				GameOver:    true,
				Reason:      "time_expired",
				Summary:     summary,
			}, nil
		}
	}

	if session.CurrentPuzzleID != req.PuzzleID {
		return nil, ErrPuzzleSessionMismatch
	}

	puzzleRecord, err := s.repo.FindPuzzleByID(ctx, req.PuzzleID)
	if err != nil || puzzleRecord == nil {
		return nil, ErrPuzzleNotFound
	}

	moves := strings.Fields(puzzleRecord.Moves)
	if len(moves) < 2 {
		return nil, errors.New("invalid puzzle moves sequence")
	}

	playedMove := strings.TrimSpace(req.Move)
	var isCorrect bool
	var nextOpponentMove string
	var puzzleSolved bool

	var currentAttemptIndex int = -1
	for i := len(session.MovesHistory) - 1; i >= 0; i-- {
		if session.MovesHistory[i].PuzzleID == puzzleRecord.ID && !session.MovesHistory[i].Solved {
			currentAttemptIndex = i
			break
		}
	}

	fullExpected := strings.Join(moves[1:], " ")
	var playerMoves []string
	for i := 1; i < len(moves); i += 2 {
		playerMoves = append(playerMoves, moves[i])
	}
	playerExpected := strings.Join(playerMoves, " ")

	if strings.EqualFold(playedMove, fullExpected) || strings.EqualFold(playedMove, playerExpected) {
		isCorrect = true
		puzzleSolved = true
	} else if currentAttemptIndex != -1 {
		lastAttempt := &session.MovesHistory[currentAttemptIndex]
		expectedIdx := len(lastAttempt.AttemptedMoves) + 1
		if expectedIdx < len(moves) && strings.EqualFold(playedMove, moves[expectedIdx]) {
			isCorrect = true
			if expectedIdx+2 < len(moves) {
				nextOpponentMove = moves[expectedIdx+1]
				lastAttempt.AttemptedMoves = append(lastAttempt.AttemptedMoves, playedMove, nextOpponentMove)
				puzzleSolved = false
			} else {
				puzzleSolved = true
				lastAttempt.Solved = true
				lastAttempt.AttemptedMoves = append(lastAttempt.AttemptedMoves, playedMove)
				lastAttempt.SolutionMoves = moves[1:]
			}
		}
	} else {
		if strings.EqualFold(playedMove, moves[1]) {
			isCorrect = true
			if len(moves) > 2 {
				nextOpponentMove = moves[2]
				session.MovesHistory = append(session.MovesHistory, RushPuzzleAttempt{
					PuzzleID:       puzzleRecord.ID,
					Solved:         false,
					AttemptedMoves: []string{playedMove, nextOpponentMove},
					SolutionMoves:  moves[1:],
					Rating:         puzzleRecord.Rating,
				})
				puzzleSolved = false
			} else {
				puzzleSolved = true
			}
		}
	}

	if isCorrect && !puzzleSolved {
		if err := s.repo.UpdateRushSessionTx(ctx, tx, session); err != nil {
			return nil, fmt.Errorf("update rush session: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}

		return &SubmitRushMoveResponse{
			Correct:     true,
			NextMove:    nextOpponentMove,
			Score:       session.Score,
			Strikes:     session.Strikes,
			StrikesLeft: 3 - session.Strikes,
			GameOver:    false,
		}, nil
	}

	solutionMoves := []string{}
	if len(moves) > 1 {
		solutionMoves = moves[1:]
	}

	if isCorrect {
		if currentAttemptIndex == -1 {
			session.MovesHistory = append(session.MovesHistory, RushPuzzleAttempt{
				PuzzleID:       puzzleRecord.ID,
				Solved:         true,
				AttemptedMoves: []string{playedMove},
				SolutionMoves:  solutionMoves,
				Rating:         puzzleRecord.Rating,
			})
		}

		session.PassedPuzzleIDs = append(session.PassedPuzzleIDs, session.CurrentPuzzleID)
		session.Score++

		minRating, maxRating := getRushRatingWindow(session.Score)
		nextPuzzle, err := s.repo.FindRushPuzzle(ctx, minRating, maxRating, session.PassedPuzzleIDs, session.Theme)
		if err != nil {
			return nil, fmt.Errorf("find next rush puzzle: %w", err)
		}

		session.CurrentPuzzleID = nextPuzzle.ID
		if err := s.repo.UpdateRushSessionTx(ctx, tx, session); err != nil {
			return nil, fmt.Errorf("update rush session: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}

		nextMoves := strings.Fields(nextPuzzle.Moves)
		initialMove := ""
		nextTotalMoves := 0
		nextPlayerMovesCount := 0
		if len(nextMoves) > 0 {
			initialMove = nextMoves[0]
			if len(nextMoves) > 1 {
				nextTotalMoves = len(nextMoves) - 1
				nextPlayerMovesCount = len(nextMoves) / 2
			}
		}

		return &SubmitRushMoveResponse{
			Correct:     true,
			Score:       session.Score,
			Strikes:     session.Strikes,
			StrikesLeft: 3 - session.Strikes,
			GameOver:    false,
			NextPuzzle: &RushPuzzleData{
				PuzzleID:         nextPuzzle.ID,
				FEN:              nextPuzzle.FEN,
				InitialMove:      initialMove,
				Rating:           nextPuzzle.Rating,
				Themes:           nextPuzzle.Themes,
				TotalMoves:       nextTotalMoves,
				PlayerMovesCount: nextPlayerMovesCount,
			},
		}, nil
	}

	if currentAttemptIndex != -1 {
		session.MovesHistory[currentAttemptIndex].Solved = false
		session.MovesHistory[currentAttemptIndex].AttemptedMoves = append(session.MovesHistory[currentAttemptIndex].AttemptedMoves, playedMove)
	} else {
		session.MovesHistory = append(session.MovesHistory, RushPuzzleAttempt{
			PuzzleID:       puzzleRecord.ID,
			Solved:         false,
			AttemptedMoves: []string{playedMove},
			SolutionMoves:  solutionMoves,
			Rating:         puzzleRecord.Rating,
		})
	}

	session.PassedPuzzleIDs = append(session.PassedPuzzleIDs, session.CurrentPuzzleID)
	session.Strikes++

	if session.Strikes >= 3 {
		session.Status = "strikes_out"
		now := time.Now().UTC()
		session.EndedAt = &now
		if err := s.repo.UpdateRushSessionTx(ctx, tx, session); err != nil {
			return nil, fmt.Errorf("update rush session: %w", err)
		}

		summary, err := s.finalizeRushSessionTx(ctx, tx, session)
		if err != nil {
			return nil, fmt.Errorf("finalize rush session: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}

		return &SubmitRushMoveResponse{
			Correct:     false,
			Score:       session.Score,
			Strikes:     session.Strikes,
			StrikesLeft: 0,
			GameOver:    true,
			Reason:      "max_strikes",
			Summary:     summary,
		}, nil
	}

	minRating, maxRating := getRushRatingWindow(session.Score)
	nextPuzzle, err := s.repo.FindRushPuzzle(ctx, minRating, maxRating, session.PassedPuzzleIDs, session.Theme)
	if err != nil {
		return nil, fmt.Errorf("find next rush puzzle: %w", err)
	}

	session.CurrentPuzzleID = nextPuzzle.ID
	if err := s.repo.UpdateRushSessionTx(ctx, tx, session); err != nil {
		return nil, fmt.Errorf("update rush session: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	nextMoves := strings.Fields(nextPuzzle.Moves)
	initialMove := ""
	nextTotalMoves := 0
	nextPlayerMovesCount := 0
	if len(nextMoves) > 0 {
		initialMove = nextMoves[0]
		if len(nextMoves) > 1 {
			nextTotalMoves = len(nextMoves) - 1
			nextPlayerMovesCount = len(nextMoves) / 2
		}
	}

	return &SubmitRushMoveResponse{
		Correct:     false,
		Score:       session.Score,
		Strikes:     session.Strikes,
		StrikesLeft: 3 - session.Strikes,
		GameOver:    false,
		NextPuzzle: &RushPuzzleData{
			PuzzleID:         nextPuzzle.ID,
			FEN:              nextPuzzle.FEN,
			InitialMove:      initialMove,
			Rating:           nextPuzzle.Rating,
			Themes:           nextPuzzle.Themes,
			TotalMoves:       nextTotalMoves,
			PlayerMovesCount: nextPlayerMovesCount,
		},
	}, nil
}

func (s *Service) EndRush(ctx context.Context, userID int64, req EndRushRequest) (*RushSummary, error) {
	s.rushLocks.lock(req.SessionID)
	defer s.rushLocks.unlock(req.SessionID)

	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	session, err := s.repo.GetRushSessionForUpdate(ctx, tx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("get rush session: %w", err)
	}
	if session == nil {
		return nil, ErrRushSessionNotFound
	}
	if session.UserID != userID {
		return nil, ErrRushSessionNotFound
	}

	if session.Status != "active" {
		return nil, ErrRushSessionEnded
	}

	session.Status = "ended"
	now := time.Now().UTC()
	session.EndedAt = &now
	if err := s.repo.UpdateRushSessionTx(ctx, tx, session); err != nil {
		return nil, fmt.Errorf("update rush session: %w", err)
	}

	summary, err := s.finalizeRushSessionTx(ctx, tx, session)
	if err != nil {
		return nil, fmt.Errorf("finalize rush session: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return summary, nil
}

func (s *Service) finalizeRushSessionTx(ctx context.Context, tx pgx.Tx, session *PuzzleRushSession) (*RushSummary, error) {
	stats, err := s.repo.GetOrCreateRushStatsForUpdate(ctx, tx, session.UserID)
	if err != nil {
		return nil, fmt.Errorf("get rush stats: %w", err)
	}

	isNewHighScore := false
	bestScore := 0

	switch session.Mode {
	case "3min":
		if session.Score > stats.BestScore3Min {
			stats.BestScore3Min = session.Score
			isNewHighScore = true
		}
		bestScore = stats.BestScore3Min
	case "5min":
		if session.Score > stats.BestScore5Min {
			stats.BestScore5Min = session.Score
			isNewHighScore = true
		}
		bestScore = stats.BestScore5Min
	case "survival":
		if session.Score > stats.BestScoreSurvival {
			stats.BestScoreSurvival = session.Score
			isNewHighScore = true
		}
		bestScore = stats.BestScoreSurvival
	}

	stats.TotalRuns++
	if err := s.repo.UpdateRushStatsTx(ctx, tx, stats); err != nil {
		return nil, fmt.Errorf("update rush stats: %w", err)
	}

	ended := time.Now().UTC()
	if session.EndedAt != nil {
		ended = *session.EndedAt
	}
	durationSec := int(ended.Sub(session.StartedAt).Seconds())
	if durationSec < 0 {
		durationSec = 0
	}

	return &RushSummary{
		SessionID:      session.ID,
		Mode:           session.Mode,
		FinalScore:     session.Score,
		TotalStrikes:   session.Strikes,
		IsNewHighScore: isNewHighScore,
		BestScore:      bestScore,
		DurationSec:    durationSec,
	}, nil
}

func (s *Service) GetRushLeaderboard(ctx context.Context, mode string, limit int) (*RushLeaderboardResponse, error) {
	switch mode {
	case "3min", "5min", "survival":
	default:
		mode = "3min"
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	entries, err := s.repo.GetRushLeaderboard(ctx, mode, limit)
	if err != nil {
		return nil, fmt.Errorf("get rush leaderboard: %w", err)
	}

	return &RushLeaderboardResponse{
		Mode:    mode,
		Entries: entries,
	}, nil
}

func (s *Service) GetUserRushStats(ctx context.Context, userID int64) (*UserRushStatsResponse, error) {
	stats, err := s.repo.GetOrCreateRushStats(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user rush stats: %w", err)
	}

	return &UserRushStatsResponse{
		BestScore3Min:     stats.BestScore3Min,
		BestScore5Min:     stats.BestScore5Min,
		BestScoreSurvival: stats.BestScoreSurvival,
		TotalRuns:         stats.TotalRuns,
	}, nil
}

func (s *Service) GetUserRushHistory(ctx context.Context, userID int64, mode string, limit, offset int) (*RushHistoryResponse, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.GetUserRushHistory(ctx, userID, mode, limit, offset)
}

func getRushRatingWindow(score int) (int, int) {
	switch {
	case score < 5:
		return 600, 1000
	case score < 10:
		return 1000, 1300
	case score < 15:
		return 1300, 1600
	case score < 20:
		return 1600, 1900
	case score < 25:
		return 1900, 2200
	default:
		return 2200, 2800
	}
}

func (s *Service) GetThemes(ctx context.Context) (*ThemesResponse, error) {
	themes, err := s.repo.GetThemes(ctx)
	if err != nil {
		return nil, fmt.Errorf("get themes: %w", err)
	}
	return &ThemesResponse{
		Themes: themes,
	}, nil
}

func (s *Service) GetCustomPuzzle(ctx context.Context, userID int64, filter CustomPuzzleFilter) (*CustomPuzzleResponse, error) {
	if filter.MinRating > 0 && filter.MaxRating > 0 && filter.MinRating > filter.MaxRating {
		return nil, ErrInvalidRatingRange
	}

	if filter.MinRating <= 0 && filter.MaxRating <= 0 {
		ratingProfile, err := s.repo.GetOrCreateUserRating(ctx, userID)
		if err == nil && ratingProfile != nil {
			filter.MinRating = ratingProfile.Rating - 100
			filter.MaxRating = ratingProfile.Rating + 100
		}
	}

	filter.UserID = userID
	puzzle, err := s.repo.FindCustomPuzzle(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("find custom puzzle: %w", err)
	}

	moves := strings.Fields(puzzle.Moves)
	initialMove := ""
	totalMoves := 0
	playerMovesCount := 0
	if len(moves) > 0 {
		initialMove = moves[0]
		if len(moves) > 1 {
			totalMoves = len(moves) - 1
			playerMovesCount = len(moves) / 2
		}
	}

	return &CustomPuzzleResponse{
		PuzzleID:         puzzle.ID,
		FEN:              puzzle.FEN,
		InitialMove:      initialMove,
		Rating:           puzzle.Rating,
		Themes:           puzzle.Themes,
		TotalMoves:       totalMoves,
		PlayerMovesCount: playerMovesCount,
	}, nil
}

func (s *Service) SubmitCustomAttempt(ctx context.Context, userID int64, req SubmitCustomAttemptRequest) (*SubmitCustomAttemptResponse, error) {
	req.Move = strings.TrimSpace(req.Move)
	req.PuzzleID = strings.TrimSpace(req.PuzzleID)

	if !isValidUCIMove(req.Move) {
		return nil, ErrInvalidUCIMove
	}

	s.userLocks.lock(userID)
	defer s.userLocks.unlock(userID)

	puzzleRecord, err := s.repo.FindPuzzleByID(ctx, req.PuzzleID)
	if err != nil || puzzleRecord == nil {
		return nil, ErrPuzzleNotFound
	}

	moves := strings.Fields(puzzleRecord.Moves)
	if len(moves) < 2 {
		return nil, errors.New("invalid puzzle moves sequence")
	}

	playedMove := strings.TrimSpace(req.Move)
	var isCorrect bool
	if len(moves) == 2 {
		isCorrect = strings.EqualFold(playedMove, moves[1])
	} else {
		fullExpected := strings.Join(moves[1:], " ")
		var playerMoves []string
		for i := 1; i < len(moves); i += 2 {
			playerMoves = append(playerMoves, moves[i])
		}
		playerExpected := strings.Join(playerMoves, " ")
		isCorrect = strings.EqualFold(playedMove, fullExpected) || strings.EqualFold(playedMove, playerExpected)
	}

	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if req.Rated {
		alreadySolved, err := s.repo.HasSolvedPuzzleTx(ctx, tx, userID, req.PuzzleID)
		if err == nil && alreadySolved {
			req.Rated = false
		}
	}

	ratingProfile, err := s.repo.GetOrCreateUserRatingForUpdate(ctx, tx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user rating: %w", err)
	}

	userRatingBefore := ratingProfile.Rating
	ratingChange := 0

	if req.Rated {
		ratingChange = calculateRatingChange(userRatingBefore, puzzleRecord.Rating, isCorrect, 3)
		ratingProfile.Rating += ratingChange
		if ratingProfile.Rating < 100 {
			ratingProfile.Rating = 100
		}
		if ratingProfile.Rating > ratingProfile.BestRating {
			ratingProfile.BestRating = ratingProfile.Rating
			ratingProfile.BestRatingAt = time.Now().UTC()
		}
		if isCorrect {
			ratingProfile.PuzzlesSolved++
		} else {
			ratingProfile.PuzzlesFailed++
		}
		if err := s.repo.UpdateUserRatingTx(ctx, tx, ratingProfile); err != nil {
			return nil, fmt.Errorf("update user rating: %w", err)
		}
	}

	solutionMoves := []string{}
	if len(moves) > 1 {
		solutionMoves = moves[1:]
	}

	attempt := &UserPuzzleAttempt{
		UserID:           userID,
		PuzzleID:         req.PuzzleID,
		Solved:           isCorrect,
		UserRatingBefore: userRatingBefore,
		UserRatingAfter:  ratingProfile.Rating,
		TimeTakenMs:      req.TimeTakenMs,
		AttemptedMoves:   []string{playedMove},
	}
	if err := s.repo.RecordAttemptTx(ctx, tx, attempt); err != nil {
		return nil, fmt.Errorf("record attempt: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &SubmitCustomAttemptResponse{
		Correct:          isCorrect,
		Solved:           isCorrect,
		PuzzleID:         req.PuzzleID,
		AttemptedMoves:   []string{playedMove},
		SolutionMoves:    solutionMoves,
		RatingChange:     ratingChange,
		UserRatingBefore: userRatingBefore,
		UserRatingAfter:  ratingProfile.Rating,
		Rated:            req.Rated,
	}, nil
}
