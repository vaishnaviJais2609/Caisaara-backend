package puzzle

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

type PuzzleService interface {
	GetDailyPuzzle(ctx context.Context, userID int64) (*DailyPuzzleResponse, error)
	SubmitAttempt(ctx context.Context, userID int64, req SubmitAttemptRequest) (*SubmitAttemptResponse, error)
	GetHint(ctx context.Context, userID int64, req HintRequest) (*HintResponse, error)
	GetSolution(ctx context.Context, userID int64, req SolutionRequest) (*SolutionResponse, error)
	GetNextPuzzle(ctx context.Context, userID int64, theme string) (*Puzzle, error)
	GetUserStats(ctx context.Context, userID int64) (*PuzzleStatsResponse, error)
	GetUserHistory(ctx context.Context, userID int64, limit, offset int) (*PuzzleHistoryResponse, error)
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

type Service struct {
	repo PuzzleRepository
}

func NewService(repo PuzzleRepository) PuzzleService {
	return &Service{
		repo: repo,
	}
}

func (s *Service) GetDailyPuzzle(ctx context.Context, userID int64) (*DailyPuzzleResponse, error) {
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

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
	if len(moves) > 0 {
		initialMove = moves[0]
	}

	return &DailyPuzzleResponse{
		PuzzleID:       puzzleRecord.ID,
		FEN:            puzzleRecord.FEN,
		InitialMove:    initialMove,
		Rating:         puzzleRecord.Rating,
		Themes:         puzzleRecord.Themes,
		AttemptsLeft:   session.AttemptsLeft,
		HintAvailable:  !session.HintUsed,
		SolutionViewed: session.SolutionViewed,
		Solved:         session.Solved,
		CurrentStreak:  ratingProfile.CurrentStreak,
		PlayerRating:   ratingProfile.Rating,
		AttemptedMoves: session.AttemptedMoves,
	}, nil
}

func (s *Service) SubmitAttempt(ctx context.Context, userID int64, req SubmitAttemptRequest) (*SubmitAttemptResponse, error) {
	today := time.Now().UTC().Format("2006-01-02")

	ratingProfile, err := s.repo.GetOrCreateUserRating(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user rating: %w", err)
	}

	session, err := s.repo.GetDailySession(ctx, userID, today)
	if err != nil {
		return nil, fmt.Errorf("get daily session: %w", err)
	}

	if session == nil {
		return nil, errors.New("daily puzzle session not found; please fetch daily puzzle first")
	}
	if session.PuzzleID != req.PuzzleID {
		return nil, errors.New("puzzle id does not match daily puzzle session")
	}

	if session.Solved {
		return &SubmitAttemptResponse{
			Correct:          true,
			Solved:           true,
			AttemptsLeft:     session.AttemptsLeft,
			RatingChange:     0,
			UserRatingBefore: ratingProfile.Rating,
			UserRatingAfter:  ratingProfile.Rating,
			CurrentStreak:    ratingProfile.CurrentStreak,
			AttemptedMoves:   session.AttemptedMoves,
			Message:          "Puzzle already solved for today.",
		}, nil
	}

	if session.AttemptsLeft <= 0 {
		return &SubmitAttemptResponse{
			Correct:          false,
			Solved:           false,
			AttemptsLeft:     0,
			RatingChange:     0,
			UserRatingBefore: ratingProfile.Rating,
			UserRatingAfter:  ratingProfile.Rating,
			CurrentStreak:    ratingProfile.CurrentStreak,
			AttemptedMoves:   session.AttemptedMoves,
			Message:          "No attempts remaining. You can view the solution.",
		}, nil
	}

	puzzleRecord, err := s.repo.FindPuzzleByID(ctx, req.PuzzleID)
	if err != nil {
		return nil, fmt.Errorf("find puzzle: %w", err)
	}

	moves := strings.Fields(puzzleRecord.Moves)
	if len(moves) < 2 {
		return nil, errors.New("invalid puzzle moves sequence")
	}

	playedMove := strings.TrimSpace(req.Move)
	recordedMoves := []string{playedMove}
	if session != nil {
		session.AttemptedMoves = append(session.AttemptedMoves, playedMove)
		recordedMoves = session.AttemptedMoves
	}

	correctMove := moves[1]
	isCorrect := strings.EqualFold(playedMove, correctMove)

	if !isCorrect && strings.Contains(playedMove, " ") {
		fullExpected := strings.Join(moves[1:], " ")
		isCorrect = strings.EqualFold(playedMove, fullExpected)
	}

	attemptsLeft := 3
	if session != nil {
		attemptsLeft = session.AttemptsLeft
	}

	if !isCorrect {
		if session != nil {
			session.AttemptsLeft--
			attemptsLeft = session.AttemptsLeft
			_ = s.repo.UpdateDailySession(ctx, session)
		}

		if attemptsLeft <= 0 {
			ratingChange := calculateRatingChange(ratingProfile.Rating, puzzleRecord.Rating, false, attemptsLeft)
			beforeRating := ratingProfile.Rating
			ratingProfile.Rating += ratingChange
			if ratingProfile.Rating < 100 {
				ratingProfile.Rating = 100
			}
			ratingProfile.PuzzlesFailed++

			_ = s.repo.UpdateUserRating(ctx, ratingProfile)

			attempt := &UserPuzzleAttempt{
				UserID:           userID,
				PuzzleID:         puzzleRecord.ID,
				Solved:           false,
				UserRatingBefore: beforeRating,
				UserRatingAfter:  ratingProfile.Rating,
				TimeTakenMs:      req.TimeTakenMs,
				AttemptedMoves:   recordedMoves,
			}
			_ = s.repo.RecordAttempt(ctx, attempt)

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

	nextMove := ""
	isFullySolved := true
	if len(moves) > 2 && !strings.Contains(playedMove, " ") {
		nextMove = moves[2]
		if len(moves) > 3 {
			isFullySolved = true
		}
	}

	ratingChange := 0
	beforeRating := ratingProfile.Rating

	if session != nil && session.SolutionViewed {
		session.Solved = true
		session.RatingEarned = 0
		_ = s.repo.UpdateDailySession(ctx, session)

		attempt := &UserPuzzleAttempt{
			UserID:           userID,
			PuzzleID:         puzzleRecord.ID,
			Solved:           true,
			UserRatingBefore: beforeRating,
			UserRatingAfter:  beforeRating,
			TimeTakenMs:      req.TimeTakenMs,
			AttemptedMoves:   recordedMoves,
		}
		_ = s.repo.RecordAttempt(ctx, attempt)

		return &SubmitAttemptResponse{
			Correct:          true,
			Solved:           true,
			NextMove:         nextMove,
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

	if ratingProfile.LastPlayedDate != today {
		ratingProfile.CurrentStreak++
		if ratingProfile.CurrentStreak > ratingProfile.MaxStreak {
			ratingProfile.MaxStreak = ratingProfile.CurrentStreak
		}
		ratingProfile.LastPlayedDate = today
	}

	_ = s.repo.UpdateUserRating(ctx, ratingProfile)

	if session != nil {
		session.Solved = true
		session.RatingEarned = ratingChange
		_ = s.repo.UpdateDailySession(ctx, session)
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
	_ = s.repo.RecordAttempt(ctx, attempt)

	return &SubmitAttemptResponse{
		Correct:          true,
		Solved:           isFullySolved,
		NextMove:         nextMove,
		AttemptsLeft:     attemptsLeft,
		RatingChange:     ratingChange,
		UserRatingBefore: beforeRating,
		UserRatingAfter:  ratingProfile.Rating,
		CurrentStreak:    ratingProfile.CurrentStreak,
		AttemptedMoves:   recordedMoves,
		Message:          "Puzzle solved successfully!",
	}, nil
}

func (s *Service) GetHint(ctx context.Context, userID int64, req HintRequest) (*HintResponse, error) {
	today := time.Now().UTC().Format("2006-01-02")

	session, err := s.repo.GetDailySession(ctx, userID, today)
	if err != nil {
		return nil, fmt.Errorf("get daily session: %w", err)
	}
	if session == nil || session.PuzzleID != req.PuzzleID {
		return nil, errors.New("daily puzzle session not found; please fetch daily puzzle first")
	}

	puzzleRecord, err := s.repo.FindPuzzleByID(ctx, req.PuzzleID)
	if err != nil {
		return nil, fmt.Errorf("find puzzle: %w", err)
	}

	moves := strings.Fields(puzzleRecord.Moves)
	if len(moves) < 2 {
		return nil, errors.New("invalid puzzle moves sequence")
	}

	correctMove := moves[1]
	fromSquare := ""
	if len(correctMove) >= 2 {
		fromSquare = correctMove[:2]
	}

	if !session.HintUsed {
		session.HintUsed = true
		_ = s.repo.UpdateDailySession(ctx, session)
	}

	return &HintResponse{
		HintType:   "piece",
		FromSquare: fromSquare,
		Message:    fmt.Sprintf("Focus on the piece on %s!", fromSquare),
		HintUsed:   true,
	}, nil
}

func (s *Service) GetSolution(ctx context.Context, userID int64, req SolutionRequest) (*SolutionResponse, error) {
	today := time.Now().UTC().Format("2006-01-02")

	session, err := s.repo.GetDailySession(ctx, userID, today)
	if err != nil {
		return nil, fmt.Errorf("get daily session: %w", err)
	}
	if session == nil || session.PuzzleID != req.PuzzleID {
		return nil, errors.New("daily puzzle session not found; please fetch daily puzzle first")
	}

	puzzleRecord, err := s.repo.FindPuzzleByID(ctx, req.PuzzleID)
	if err != nil {
		return nil, fmt.Errorf("find puzzle: %w", err)
	}

	moves := strings.Fields(puzzleRecord.Moves)
	solutionMoves := []string{}
	if len(moves) > 1 {
		solutionMoves = moves[1:]
	}

	session.SolutionViewed = true
	_ = s.repo.UpdateDailySession(ctx, session)

	return &SolutionResponse{
		Moves:          solutionMoves,
		RatingChange:   0,
		SolutionViewed: true,
		Message:        "Solution revealed. No rating increase will be awarded.",
	}, nil
}

func (s *Service) GetNextPuzzle(ctx context.Context, userID int64, theme string) (*Puzzle, error) {
	ratingProfile, err := s.repo.GetOrCreateUserRating(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user rating: %w", err)
	}

	minRating := ratingProfile.Rating - 75
	maxRating := ratingProfile.Rating + 75

	return s.repo.FindMatchingPuzzle(ctx, userID, minRating, maxRating, theme)
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
	var timeLimitSec int
	switch req.Mode {
	case "3min":
		timeLimitSec = 180
	case "5min":
		timeLimitSec = 300
	case "survival":
		timeLimitSec = 0
	default:
		return nil, errors.New("invalid mode: must be 3min, 5min, or survival")
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
	if len(moves) > 0 {
		initialMove = moves[0]
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
			PuzzleID:    puzzle.ID,
			FEN:         puzzle.FEN,
			InitialMove: initialMove,
			Rating:      puzzle.Rating,
			Themes:      puzzle.Themes,
		},
	}, nil
}

func (s *Service) SubmitRushMove(ctx context.Context, userID int64, req SubmitRushMoveRequest) (*SubmitRushMoveResponse, error) {
	session, err := s.repo.GetRushSession(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("get rush session: %w", err)
	}
	if session == nil {
		return nil, errors.New("rush session not found")
	}
	if session.UserID != userID {
		return nil, errors.New("unauthorized session access")
	}

	if session.Status != "active" {
		return &SubmitRushMoveResponse{
			Score:       session.Score,
			Strikes:     session.Strikes,
			StrikesLeft: 0,
			GameOver:    true,
			Reason:      session.Status,
		}, nil
	}

	if session.TimeLimitSec > 0 {
		elapsed := time.Since(session.StartedAt).Seconds()
		if elapsed > float64(session.TimeLimitSec+2) {
			session.Status = "time_expired"
			now := time.Now().UTC()
			session.EndedAt = &now
			if err := s.repo.UpdateRushSession(ctx, session); err != nil {
				return nil, fmt.Errorf("update rush session: %w", err)
			}

			summary, err := s.finalizeRushSession(ctx, session)
			if err != nil {
				return nil, fmt.Errorf("finalize rush session: %w", err)
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
		return nil, errors.New("puzzle id does not match current session puzzle")
	}

	puzzleRecord, err := s.repo.FindPuzzleByID(ctx, req.PuzzleID)
	if err != nil {
		return nil, fmt.Errorf("find puzzle: %w", err)
	}

	moves := strings.Fields(puzzleRecord.Moves)
	if len(moves) < 2 {
		return nil, errors.New("invalid puzzle moves sequence")
	}

	playedMove := strings.TrimSpace(req.Move)
	correctMove := moves[1]
	isCorrect := strings.EqualFold(playedMove, correctMove)
	if !isCorrect && strings.Contains(playedMove, " ") {
		isCorrect = strings.EqualFold(playedMove, strings.Join(moves[1:], " "))
	}

	solutionMoves := []string{}
	if len(moves) > 1 {
		solutionMoves = moves[1:]
	}

	session.MovesHistory = append(session.MovesHistory, RushPuzzleAttempt{
		PuzzleID:       puzzleRecord.ID,
		Solved:         isCorrect,
		AttemptedMoves: []string{playedMove},
		SolutionMoves:  solutionMoves,
		Rating:         puzzleRecord.Rating,
	})

	session.PassedPuzzleIDs = append(session.PassedPuzzleIDs, session.CurrentPuzzleID)

	if isCorrect {
		session.Score++
		minRating, maxRating := getRushRatingWindow(session.Score)
		nextPuzzle, err := s.repo.FindRushPuzzle(ctx, minRating, maxRating, session.PassedPuzzleIDs, session.Theme)
		if err != nil {
			return nil, fmt.Errorf("find next rush puzzle: %w", err)
		}

		session.CurrentPuzzleID = nextPuzzle.ID
		if err := s.repo.UpdateRushSession(ctx, session); err != nil {
			return nil, fmt.Errorf("update rush session: %w", err)
		}

		nextMoves := strings.Fields(nextPuzzle.Moves)
		initialMove := ""
		if len(nextMoves) > 0 {
			initialMove = nextMoves[0]
		}

		return &SubmitRushMoveResponse{
			Correct:     true,
			Score:       session.Score,
			Strikes:     session.Strikes,
			StrikesLeft: 3 - session.Strikes,
			GameOver:    false,
			NextPuzzle: &RushPuzzleData{
				PuzzleID:    nextPuzzle.ID,
				FEN:         nextPuzzle.FEN,
				InitialMove: initialMove,
				Rating:      nextPuzzle.Rating,
				Themes:      nextPuzzle.Themes,
			},
		}, nil
	}

	session.Strikes++
	if session.Strikes >= 3 {
		session.Status = "strikes_out"
		now := time.Now().UTC()
		session.EndedAt = &now
		if err := s.repo.UpdateRushSession(ctx, session); err != nil {
			return nil, fmt.Errorf("update rush session: %w", err)
		}

		summary, err := s.finalizeRushSession(ctx, session)
		if err != nil {
			return nil, fmt.Errorf("finalize rush session: %w", err)
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
	if err := s.repo.UpdateRushSession(ctx, session); err != nil {
		return nil, fmt.Errorf("update rush session: %w", err)
	}

	nextMoves := strings.Fields(nextPuzzle.Moves)
	initialMove := ""
	if len(nextMoves) > 0 {
		initialMove = nextMoves[0]
	}

	return &SubmitRushMoveResponse{
		Correct:     false,
		Score:       session.Score,
		Strikes:     session.Strikes,
		StrikesLeft: 3 - session.Strikes,
		GameOver:    false,
		NextPuzzle: &RushPuzzleData{
			PuzzleID:    nextPuzzle.ID,
			FEN:         nextPuzzle.FEN,
			InitialMove: initialMove,
			Rating:      nextPuzzle.Rating,
			Themes:      nextPuzzle.Themes,
		},
	}, nil
}

func (s *Service) EndRush(ctx context.Context, userID int64, req EndRushRequest) (*RushSummary, error) {
	session, err := s.repo.GetRushSession(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("get rush session: %w", err)
	}
	if session == nil {
		return nil, errors.New("rush session not found")
	}
	if session.UserID != userID {
		return nil, errors.New("unauthorized session access")
	}

	if session.Status != "active" {
		stats, err := s.repo.GetOrCreateRushStats(ctx, session.UserID)
		if err != nil {
			return nil, fmt.Errorf("get rush stats: %w", err)
		}
		bestScore := 0
		switch session.Mode {
		case "3min":
			bestScore = stats.BestScore3Min
		case "5min":
			bestScore = stats.BestScore5Min
		case "survival":
			bestScore = stats.BestScoreSurvival
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
			IsNewHighScore: false,
			BestScore:      bestScore,
			DurationSec:    durationSec,
		}, nil
	}

	session.Status = "ended"
	now := time.Now().UTC()
	session.EndedAt = &now
	if err := s.repo.UpdateRushSession(ctx, session); err != nil {
		return nil, fmt.Errorf("update rush session: %w", err)
	}

	return s.finalizeRushSession(ctx, session)
}

func (s *Service) finalizeRushSession(ctx context.Context, session *PuzzleRushSession) (*RushSummary, error) {
	stats, err := s.repo.GetOrCreateRushStats(ctx, session.UserID)
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
	if err := s.repo.UpdateRushStats(ctx, stats); err != nil {
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
	if len(moves) > 0 {
		initialMove = moves[0]
	}

	return &CustomPuzzleResponse{
		PuzzleID:    puzzle.ID,
		FEN:         puzzle.FEN,
		InitialMove: initialMove,
		Rating:      puzzle.Rating,
		Themes:      puzzle.Themes,
	}, nil
}

func (s *Service) SubmitCustomAttempt(ctx context.Context, userID int64, req SubmitCustomAttemptRequest) (*SubmitCustomAttemptResponse, error) {
	puzzleRecord, err := s.repo.FindPuzzleByID(ctx, req.PuzzleID)
	if err != nil {
		return nil, fmt.Errorf("find puzzle: %w", err)
	}

	moves := strings.Fields(puzzleRecord.Moves)
	if len(moves) < 2 {
		return nil, errors.New("invalid puzzle moves sequence")
	}

	playedMove := strings.TrimSpace(req.Move)
	correctMove := moves[1]
	isCorrect := strings.EqualFold(playedMove, correctMove)
	if !isCorrect && strings.Contains(playedMove, " ") {
		isCorrect = strings.EqualFold(playedMove, strings.Join(moves[1:], " "))
	}

	ratingProfile, err := s.repo.GetOrCreateUserRating(ctx, userID)
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
		_ = s.repo.UpdateUserRating(ctx, ratingProfile)
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
	_ = s.repo.RecordAttempt(ctx, attempt)

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
