package puzzle

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type PuzzleRepository interface {
	FindMatchingPuzzle(ctx context.Context, userID int64, minRating, maxRating int, theme string) (*Puzzle, error)
	FindPuzzleByID(ctx context.Context, id string) (*Puzzle, error)
	GetOrCreateUserRating(ctx context.Context, userID int64) (*UserPuzzleRating, error)
	UpdateUserRating(ctx context.Context, rating *UserPuzzleRating) error
	RecordAttempt(ctx context.Context, attempt *UserPuzzleAttempt) error
	GetDailySession(ctx context.Context, userID int64, date string) (*DailyPuzzleSession, error)
	CreateDailySession(ctx context.Context, session *DailyPuzzleSession) error
	UpdateDailySession(ctx context.Context, session *DailyPuzzleSession) error
	GetUserStats(ctx context.Context, userID int64) (*PuzzleStatsResponse, error)
	GetUserHistory(ctx context.Context, userID int64, limit, offset int) (*PuzzleHistoryResponse, error)
	CreateRushSession(ctx context.Context, session *PuzzleRushSession) error
	GetRushSession(ctx context.Context, sessionID string) (*PuzzleRushSession, error)
	UpdateRushSession(ctx context.Context, session *PuzzleRushSession) error
	FindRushPuzzle(ctx context.Context, minRating, maxRating int, excludedIDs []string, theme string) (*Puzzle, error)
	GetOrCreateRushStats(ctx context.Context, userID int64) (*UserPuzzleRushStats, error)
	UpdateRushStats(ctx context.Context, stats *UserPuzzleRushStats) error
	GetRushLeaderboard(ctx context.Context, mode string, limit int) ([]RushLeaderboardEntry, error)
	GetUserRushHistory(ctx context.Context, userID int64, mode string, limit, offset int) (*RushHistoryResponse, error)
	GetThemes(ctx context.Context) ([]PuzzleThemeInfo, error)
	FindCustomPuzzle(ctx context.Context, filter CustomPuzzleFilter) (*Puzzle, error)
	HasSolvedPuzzle(ctx context.Context, userID int64, puzzleID string) (bool, error)
	Begin(ctx context.Context) (pgx.Tx, error)
	GetDailySessionForUpdate(ctx context.Context, tx pgx.Tx, userID int64, date string) (*DailyPuzzleSession, error)
	UpdateDailySessionTx(ctx context.Context, tx pgx.Tx, session *DailyPuzzleSession) error
	GetOrCreateUserRatingForUpdate(ctx context.Context, tx pgx.Tx, userID int64) (*UserPuzzleRating, error)
	UpdateUserRatingTx(ctx context.Context, tx pgx.Tx, rating *UserPuzzleRating) error
	RecordAttemptTx(ctx context.Context, tx pgx.Tx, attempt *UserPuzzleAttempt) error
	HasSolvedPuzzleTx(ctx context.Context, tx pgx.Tx, userID int64, puzzleID string) (bool, error)
	GetRushSessionForUpdate(ctx context.Context, tx pgx.Tx, sessionID string) (*PuzzleRushSession, error)
	UpdateRushSessionTx(ctx context.Context, tx pgx.Tx, session *PuzzleRushSession) error
	GetOrCreateRushStatsForUpdate(ctx context.Context, tx pgx.Tx, userID int64) (*UserPuzzleRushStats, error)
	UpdateRushStatsTx(ctx context.Context, tx pgx.Tx, stats *UserPuzzleRushStats) error
}

type Repository struct {
	DB           *pgxpool.Pool
	Redis        *redis.Client
	themesCache  []PuzzleThemeInfo
	themesExpire time.Time
	themesMu     sync.RWMutex
}

func (r *Repository) Begin(ctx context.Context) (pgx.Tx, error) {
	return r.DB.Begin(ctx)
}

func NewRepository(db *pgxpool.Pool, redisClient ...*redis.Client) PuzzleRepository {
	var rClient *redis.Client
	if len(redisClient) > 0 {
		rClient = redisClient[0]
	}
	return &Repository{
		DB:    db,
		Redis: rClient,
	}
}

func (r *Repository) FindMatchingPuzzle(ctx context.Context, userID int64, minRating, maxRating int, theme string) (*Puzzle, error) {
	offset := rand.IntN(100)
	query := `
		WITH candidates AS (
			SELECT id, fen, moves, rating, rating_deviation, popularity, nb_plays, themes, COALESCE(game_url, ''), COALESCE(opening_tags, ''), created_at
			FROM puzzles
			WHERE rating BETWEEN $1 AND $2
			  AND ($3 = '' OR $3 = ANY(themes))
			  AND id NOT IN (
			      SELECT puzzle_id FROM user_puzzle_attempts WHERE user_id = $4
			  )
			OFFSET $5
			LIMIT 50
		)
		SELECT * FROM candidates ORDER BY RANDOM() LIMIT 1
	`

	p := &Puzzle{}
	err := r.DB.QueryRow(ctx, query, minRating, maxRating, theme, userID, offset).Scan(
		&p.ID,
		&p.FEN,
		&p.Moves,
		&p.Rating,
		&p.RatingDeviation,
		&p.Popularity,
		&p.NbPlays,
		&p.Themes,
		&p.GameURL,
		&p.OpeningTags,
		&p.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) && offset > 0 {
		err = r.DB.QueryRow(ctx, query, minRating, maxRating, theme, userID, 0).Scan(
			&p.ID,
			&p.FEN,
			&p.Moves,
			&p.Rating,
			&p.RatingDeviation,
			&p.Popularity,
			&p.NbPlays,
			&p.Themes,
			&p.GameURL,
			&p.OpeningTags,
			&p.CreatedAt,
		)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		fallbackQuery := `
			WITH candidates AS (
				SELECT id, fen, moves, rating, rating_deviation, popularity, nb_plays, themes, COALESCE(game_url, ''), COALESCE(opening_tags, ''), created_at
				FROM puzzles
				WHERE rating BETWEEN $1 AND $2
				LIMIT 50
			)
			SELECT * FROM candidates ORDER BY RANDOM() LIMIT 1
		`
		err = r.DB.QueryRow(ctx, fallbackQuery, minRating-150, maxRating+150).Scan(
			&p.ID,
			&p.FEN,
			&p.Moves,
			&p.Rating,
			&p.RatingDeviation,
			&p.Popularity,
			&p.NbPlays,
			&p.Themes,
			&p.GameURL,
			&p.OpeningTags,
			&p.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return p, nil
}

func (r *Repository) FindPuzzleByID(ctx context.Context, id string) (*Puzzle, error) {
	query := `
		SELECT id, fen, moves, rating, rating_deviation, popularity, nb_plays, themes, COALESCE(game_url, ''), COALESCE(opening_tags, ''), created_at
		FROM puzzles
		WHERE id = $1
	`

	p := &Puzzle{}
	err := r.DB.QueryRow(ctx, query, id).Scan(
		&p.ID,
		&p.FEN,
		&p.Moves,
		&p.Rating,
		&p.RatingDeviation,
		&p.Popularity,
		&p.NbPlays,
		&p.Themes,
		&p.GameURL,
		&p.OpeningTags,
		&p.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	return p, nil
}

func (r *Repository) GetOrCreateUserRating(ctx context.Context, userID int64) (*UserPuzzleRating, error) {
	query := `
		SELECT user_id, rating, rating_deviation, rating_volatility, puzzles_solved, puzzles_failed, best_rating, best_rating_at, current_streak, max_streak, last_played_date, updated_at
		FROM user_puzzle_ratings
		WHERE user_id = $1
	`

	u := &UserPuzzleRating{}
	var lastPlayedDate *time.Time

	err := r.DB.QueryRow(ctx, query, userID).Scan(
		&u.UserID,
		&u.Rating,
		&u.RatingDeviation,
		&u.RatingVolatility,
		&u.PuzzlesSolved,
		&u.PuzzlesFailed,
		&u.BestRating,
		&u.BestRatingAt,
		&u.CurrentStreak,
		&u.MaxStreak,
		&lastPlayedDate,
		&u.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		insertQuery := `
			INSERT INTO user_puzzle_ratings (
				user_id, rating, rating_deviation, rating_volatility, puzzles_solved, puzzles_failed, best_rating, best_rating_at, current_streak, max_streak, updated_at
			)
			VALUES ($1, 1500, 350, 0.06, 0, 0, 1500, NOW(), 0, 0, NOW())
			ON CONFLICT (user_id) DO UPDATE SET updated_at = NOW()
			RETURNING user_id, rating, rating_deviation, rating_volatility, puzzles_solved, puzzles_failed, best_rating, best_rating_at, current_streak, max_streak, last_played_date, updated_at
		`
		err = r.DB.QueryRow(ctx, insertQuery, userID).Scan(
			&u.UserID,
			&u.Rating,
			&u.RatingDeviation,
			&u.RatingVolatility,
			&u.PuzzlesSolved,
			&u.PuzzlesFailed,
			&u.BestRating,
			&u.BestRatingAt,
			&u.CurrentStreak,
			&u.MaxStreak,
			&lastPlayedDate,
			&u.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	if lastPlayedDate != nil {
		u.LastPlayedDate = lastPlayedDate.Format("2006-01-02")
	}

	return u, nil
}

func (r *Repository) UpdateUserRating(ctx context.Context, rating *UserPuzzleRating) error {
	var lpd *time.Time
	if rating.LastPlayedDate != "" {
		parsed, err := time.Parse("2006-01-02", rating.LastPlayedDate)
		if err == nil {
			lpd = &parsed
		}
	}

	query := `
		UPDATE user_puzzle_ratings
		SET rating = $1,
			rating_deviation = $2,
			rating_volatility = $3,
			puzzles_solved = $4,
			puzzles_failed = $5,
			best_rating = $6,
			best_rating_at = $7,
			current_streak = $8,
			max_streak = $9,
			last_played_date = $10,
			updated_at = NOW()
		WHERE user_id = $11
	`

	_, err := r.DB.Exec(
		ctx,
		query,
		rating.Rating,
		rating.RatingDeviation,
		rating.RatingVolatility,
		rating.PuzzlesSolved,
		rating.PuzzlesFailed,
		rating.BestRating,
		rating.BestRatingAt,
		rating.CurrentStreak,
		rating.MaxStreak,
		lpd,
		rating.UserID,
	)

	return err
}

func (r *Repository) GetOrCreateUserRatingForUpdate(ctx context.Context, tx pgx.Tx, userID int64) (*UserPuzzleRating, error) {
	query := `
		SELECT user_id, rating, rating_deviation, rating_volatility, puzzles_solved, puzzles_failed, best_rating, best_rating_at, current_streak, max_streak, last_played_date, updated_at
		FROM user_puzzle_ratings
		WHERE user_id = $1
		FOR UPDATE
	`

	u := &UserPuzzleRating{}
	var lastPlayedDate *time.Time

	err := tx.QueryRow(ctx, query, userID).Scan(
		&u.UserID,
		&u.Rating,
		&u.RatingDeviation,
		&u.RatingVolatility,
		&u.PuzzlesSolved,
		&u.PuzzlesFailed,
		&u.BestRating,
		&u.BestRatingAt,
		&u.CurrentStreak,
		&u.MaxStreak,
		&lastPlayedDate,
		&u.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		insertQuery := `
			INSERT INTO user_puzzle_ratings (
				user_id, rating, rating_deviation, rating_volatility, puzzles_solved, puzzles_failed, best_rating, best_rating_at, current_streak, max_streak, updated_at
			)
			VALUES ($1, 1500, 350, 0.06, 0, 0, 1500, NOW(), 0, 0, NOW())
			ON CONFLICT (user_id) DO UPDATE SET updated_at = NOW()
			RETURNING user_id, rating, rating_deviation, rating_volatility, puzzles_solved, puzzles_failed, best_rating, best_rating_at, current_streak, max_streak, last_played_date, updated_at
		`
		err = tx.QueryRow(ctx, insertQuery, userID).Scan(
			&u.UserID,
			&u.Rating,
			&u.RatingDeviation,
			&u.RatingVolatility,
			&u.PuzzlesSolved,
			&u.PuzzlesFailed,
			&u.BestRating,
			&u.BestRatingAt,
			&u.CurrentStreak,
			&u.MaxStreak,
			&lastPlayedDate,
			&u.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	if lastPlayedDate != nil {
		u.LastPlayedDate = lastPlayedDate.Format("2006-01-02")
	}

	return u, nil
}

func (r *Repository) UpdateUserRatingTx(ctx context.Context, tx pgx.Tx, rating *UserPuzzleRating) error {
	var lpd *time.Time
	if rating.LastPlayedDate != "" {
		parsed, err := time.Parse("2006-01-02", rating.LastPlayedDate)
		if err == nil {
			lpd = &parsed
		}
	}

	query := `
		UPDATE user_puzzle_ratings
		SET rating = $1,
			rating_deviation = $2,
			rating_volatility = $3,
			puzzles_solved = $4,
			puzzles_failed = $5,
			best_rating = $6,
			best_rating_at = $7,
			current_streak = $8,
			max_streak = $9,
			last_played_date = $10,
			updated_at = NOW()
		WHERE user_id = $11
	`

	_, err := tx.Exec(
		ctx,
		query,
		rating.Rating,
		rating.RatingDeviation,
		rating.RatingVolatility,
		rating.PuzzlesSolved,
		rating.PuzzlesFailed,
		rating.BestRating,
		rating.BestRatingAt,
		rating.CurrentStreak,
		rating.MaxStreak,
		lpd,
		rating.UserID,
	)

	return err
}

func (r *Repository) RecordAttempt(ctx context.Context, attempt *UserPuzzleAttempt) error {
	query := `
		INSERT INTO user_puzzle_attempts (
			user_id, puzzle_id, solved, user_rating_before, user_rating_after, time_taken_ms, attempted_moves, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		RETURNING id, created_at
	`

	if attempt.AttemptedMoves == nil {
		attempt.AttemptedMoves = []string{}
	}

	return r.DB.QueryRow(
		ctx,
		query,
		attempt.UserID,
		attempt.PuzzleID,
		attempt.Solved,
		attempt.UserRatingBefore,
		attempt.UserRatingAfter,
		attempt.TimeTakenMs,
		attempt.AttemptedMoves,
	).Scan(&attempt.ID, &attempt.CreatedAt)
}

func (r *Repository) HasSolvedPuzzle(ctx context.Context, userID int64, puzzleID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM user_puzzle_attempts WHERE user_id = $1 AND puzzle_id = $2 AND solved = true)`
	var exists bool
	err := r.DB.QueryRow(ctx, query, userID, puzzleID).Scan(&exists)
	return exists, err
}

func (r *Repository) RecordAttemptTx(ctx context.Context, tx pgx.Tx, attempt *UserPuzzleAttempt) error {
	query := `
		INSERT INTO user_puzzle_attempts (
			user_id, puzzle_id, solved, user_rating_before, user_rating_after, time_taken_ms, attempted_moves, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		RETURNING id, created_at
	`

	if attempt.AttemptedMoves == nil {
		attempt.AttemptedMoves = []string{}
	}

	return tx.QueryRow(
		ctx,
		query,
		attempt.UserID,
		attempt.PuzzleID,
		attempt.Solved,
		attempt.UserRatingBefore,
		attempt.UserRatingAfter,
		attempt.TimeTakenMs,
		attempt.AttemptedMoves,
	).Scan(&attempt.ID, &attempt.CreatedAt)
}

func (r *Repository) HasSolvedPuzzleTx(ctx context.Context, tx pgx.Tx, userID int64, puzzleID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM user_puzzle_attempts WHERE user_id = $1 AND puzzle_id = $2 AND solved = true)`
	var exists bool
	err := tx.QueryRow(ctx, query, userID, puzzleID).Scan(&exists)
	return exists, err
}

func (r *Repository) GetDailySession(ctx context.Context, userID int64, date string) (*DailyPuzzleSession, error) {
	query := `
		SELECT user_id, puzzle_id, TO_CHAR(date, 'YYYY-MM-DD'), attempts_left, hint_used, solution_viewed, solved, rating_earned, COALESCE(attempted_moves, '{}')
		FROM daily_puzzle_sessions
		WHERE user_id = $1 AND date = $2::date
	`

	session := &DailyPuzzleSession{}
	err := r.DB.QueryRow(ctx, query, userID, date).Scan(
		&session.UserID,
		&session.PuzzleID,
		&session.Date,
		&session.AttemptsLeft,
		&session.HintUsed,
		&session.SolutionViewed,
		&session.Solved,
		&session.RatingEarned,
		&session.AttemptedMoves,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if session.AttemptedMoves == nil {
		session.AttemptedMoves = []string{}
	}

	return session, nil
}

func (r *Repository) CreateDailySession(ctx context.Context, session *DailyPuzzleSession) error {
	query := `
		INSERT INTO daily_puzzle_sessions (
			user_id, puzzle_id, date, attempts_left, hint_used, solution_viewed, solved, rating_earned, attempted_moves, created_at, updated_at
		)
		VALUES ($1, $2, $3::date, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		ON CONFLICT (user_id, date) DO NOTHING
	`

	if session.AttemptedMoves == nil {
		session.AttemptedMoves = []string{}
	}

	_, err := r.DB.Exec(
		ctx,
		query,
		session.UserID,
		session.PuzzleID,
		session.Date,
		session.AttemptsLeft,
		session.HintUsed,
		session.SolutionViewed,
		session.Solved,
		session.RatingEarned,
		session.AttemptedMoves,
	)

	return err
}

func (r *Repository) UpdateDailySession(ctx context.Context, session *DailyPuzzleSession) error {
	query := `
		UPDATE daily_puzzle_sessions
		SET attempts_left = $1,
			hint_used = $2,
			solution_viewed = $3,
			solved = $4,
			rating_earned = $5,
			attempted_moves = $6,
			updated_at = NOW()
		WHERE user_id = $7 AND date = $8::date
	`

	if session.AttemptedMoves == nil {
		session.AttemptedMoves = []string{}
	}

	_, err := r.DB.Exec(
		ctx,
		query,
		session.AttemptsLeft,
		session.HintUsed,
		session.SolutionViewed,
		session.Solved,
		session.RatingEarned,
		session.AttemptedMoves,
		session.UserID,
		session.Date,
	)

	return err
}

func (r *Repository) GetDailySessionForUpdate(ctx context.Context, tx pgx.Tx, userID int64, date string) (*DailyPuzzleSession, error) {
	query := `
		SELECT user_id, puzzle_id, TO_CHAR(date, 'YYYY-MM-DD'), attempts_left, hint_used, solution_viewed, solved, rating_earned, COALESCE(attempted_moves, '{}')
		FROM daily_puzzle_sessions
		WHERE user_id = $1 AND date = $2::date
		FOR UPDATE
	`

	session := &DailyPuzzleSession{}
	err := tx.QueryRow(ctx, query, userID, date).Scan(
		&session.UserID,
		&session.PuzzleID,
		&session.Date,
		&session.AttemptsLeft,
		&session.HintUsed,
		&session.SolutionViewed,
		&session.Solved,
		&session.RatingEarned,
		&session.AttemptedMoves,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if session.AttemptedMoves == nil {
		session.AttemptedMoves = []string{}
	}

	return session, nil
}

func (r *Repository) UpdateDailySessionTx(ctx context.Context, tx pgx.Tx, session *DailyPuzzleSession) error {
	query := `
		UPDATE daily_puzzle_sessions
		SET attempts_left = $1,
			hint_used = $2,
			solution_viewed = $3,
			solved = $4,
			rating_earned = $5,
			attempted_moves = $6,
			updated_at = NOW()
		WHERE user_id = $7 AND date = $8::date
	`

	if session.AttemptedMoves == nil {
		session.AttemptedMoves = []string{}
	}

	_, err := tx.Exec(
		ctx,
		query,
		session.AttemptsLeft,
		session.HintUsed,
		session.SolutionViewed,
		session.Solved,
		session.RatingEarned,
		session.AttemptedMoves,
		session.UserID,
		session.Date,
	)

	return err
}

func (r *Repository) GetUserStats(ctx context.Context, userID int64) (*PuzzleStatsResponse, error) {
	rating, err := r.GetOrCreateUserRating(ctx, userID)
	if err != nil {
		return nil, err
	}

	total := rating.PuzzlesSolved + rating.PuzzlesFailed
	winRate := 0.0
	if total > 0 {
		winRate = (float64(rating.PuzzlesSolved) / float64(total)) * 100.0
	}

	stats := &PuzzleStatsResponse{
		Rating:        rating.Rating,
		BestRating:    rating.BestRating,
		BestRatingAt:  &rating.BestRatingAt,
		PuzzlesSolved: rating.PuzzlesSolved,
		PuzzlesFailed: rating.PuzzlesFailed,
		WinRate:       winRate,
		CurrentStreak: rating.CurrentStreak,
		MaxStreak:     rating.MaxStreak,
	}

	return stats, nil
}

func (r *Repository) GetUserHistory(ctx context.Context, userID int64, limit, offset int) (*PuzzleHistoryResponse, error) {
	var total int64
	countQuery := `SELECT COUNT(*) FROM user_puzzle_attempts WHERE user_id = $1`
	if err := r.DB.QueryRow(ctx, countQuery, userID).Scan(&total); err != nil {
		return nil, err
	}

	query := `
		SELECT a.id, a.puzzle_id, a.solved, COALESCE(a.attempted_moves, '{}'), COALESCE(p.moves, ''),
		       a.user_rating_before, a.user_rating_after, (a.user_rating_after - a.user_rating_before) AS rating_change,
		       a.time_taken_ms, a.created_at
		FROM user_puzzle_attempts a
		LEFT JOIN puzzles p ON a.puzzle_id = p.id
		WHERE a.user_id = $1
		ORDER BY a.created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.DB.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PuzzleHistoryItem, 0)
	for rows.Next() {
		var item PuzzleHistoryItem
		var rawMoves string
		if err := rows.Scan(
			&item.ID,
			&item.PuzzleID,
			&item.Solved,
			&item.AttemptedMoves,
			&rawMoves,
			&item.UserRatingBefore,
			&item.UserRatingAfter,
			&item.RatingChange,
			&item.TimeTakenMs,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}

		if item.AttemptedMoves == nil {
			item.AttemptedMoves = []string{}
		}

		parts := strings.Fields(rawMoves)
		switch {
		case len(parts) > 1:
			item.SolutionMoves = parts[1:]
		case len(parts) == 1:
			item.SolutionMoves = parts
		default:
			item.SolutionMoves = []string{}
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &PuzzleHistoryResponse{
		Total:    total,
		Attempts: items,
	}, nil
}

func (r *Repository) CreateRushSession(ctx context.Context, session *PuzzleRushSession) error {
	query := `
		INSERT INTO puzzle_rush_sessions (
			id, user_id, mode, score, strikes, current_puzzle_id, passed_puzzle_ids, status, time_limit_sec, theme, moves_history, started_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW())
	`

	if session.PassedPuzzleIDs == nil {
		session.PassedPuzzleIDs = []string{}
	}
	if session.MovesHistory == nil {
		session.MovesHistory = []RushPuzzleAttempt{}
	}

	movesJSON, err := json.Marshal(session.MovesHistory)
	if err != nil {
		movesJSON = []byte("[]")
	}

	_, err = r.DB.Exec(
		ctx,
		query,
		session.ID,
		session.UserID,
		session.Mode,
		session.Score,
		session.Strikes,
		session.CurrentPuzzleID,
		session.PassedPuzzleIDs,
		session.Status,
		session.TimeLimitSec,
		session.Theme,
		movesJSON,
		session.StartedAt,
	)

	return err
}

func (r *Repository) GetRushSession(ctx context.Context, sessionID string) (*PuzzleRushSession, error) {
	query := `
		SELECT id, user_id, mode, score, strikes, current_puzzle_id, passed_puzzle_ids, status, time_limit_sec, theme, started_at, ended_at, created_at, updated_at, COALESCE(moves_history, '[]'::jsonb)
		FROM puzzle_rush_sessions
		WHERE id = $1::uuid
	`

	session := &PuzzleRushSession{}
	var movesData []byte
	err := r.DB.QueryRow(ctx, query, sessionID).Scan(
		&session.ID,
		&session.UserID,
		&session.Mode,
		&session.Score,
		&session.Strikes,
		&session.CurrentPuzzleID,
		&session.PassedPuzzleIDs,
		&session.Status,
		&session.TimeLimitSec,
		&session.Theme,
		&session.StartedAt,
		&session.EndedAt,
		&session.CreatedAt,
		&session.UpdatedAt,
		&movesData,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if session.PassedPuzzleIDs == nil {
		session.PassedPuzzleIDs = []string{}
	}

	if len(movesData) > 0 {
		_ = json.Unmarshal(movesData, &session.MovesHistory)
	}
	if session.MovesHistory == nil {
		session.MovesHistory = []RushPuzzleAttempt{}
	}

	return session, nil
}

func (r *Repository) UpdateRushSession(ctx context.Context, session *PuzzleRushSession) error {
	query := `
		UPDATE puzzle_rush_sessions
		SET score = $1,
			strikes = $2,
			current_puzzle_id = $3,
			passed_puzzle_ids = $4,
			status = $5,
			ended_at = $6,
			moves_history = $7,
			updated_at = NOW()
		WHERE id = $8::uuid
	`

	if session.PassedPuzzleIDs == nil {
		session.PassedPuzzleIDs = []string{}
	}
	if session.MovesHistory == nil {
		session.MovesHistory = []RushPuzzleAttempt{}
	}

	movesJSON, err := json.Marshal(session.MovesHistory)
	if err != nil {
		movesJSON = []byte("[]")
	}

	_, err = r.DB.Exec(
		ctx,
		query,
		session.Score,
		session.Strikes,
		session.CurrentPuzzleID,
		session.PassedPuzzleIDs,
		session.Status,
		session.EndedAt,
		movesJSON,
		session.ID,
	)

	return err
}

func (r *Repository) GetRushSessionForUpdate(ctx context.Context, tx pgx.Tx, sessionID string) (*PuzzleRushSession, error) {
	query := `
		SELECT id, user_id, mode, score, strikes, current_puzzle_id, passed_puzzle_ids, status, time_limit_sec, theme, started_at, ended_at, created_at, updated_at, COALESCE(moves_history, '[]'::jsonb)
		FROM puzzle_rush_sessions
		WHERE id = $1::uuid
		FOR UPDATE
	`

	session := &PuzzleRushSession{}
	var movesData []byte
	err := tx.QueryRow(ctx, query, sessionID).Scan(
		&session.ID,
		&session.UserID,
		&session.Mode,
		&session.Score,
		&session.Strikes,
		&session.CurrentPuzzleID,
		&session.PassedPuzzleIDs,
		&session.Status,
		&session.TimeLimitSec,
		&session.Theme,
		&session.StartedAt,
		&session.EndedAt,
		&session.CreatedAt,
		&session.UpdatedAt,
		&movesData,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if session.PassedPuzzleIDs == nil {
		session.PassedPuzzleIDs = []string{}
	}

	if len(movesData) > 0 {
		_ = json.Unmarshal(movesData, &session.MovesHistory)
	}
	if session.MovesHistory == nil {
		session.MovesHistory = []RushPuzzleAttempt{}
	}

	return session, nil
}

func (r *Repository) UpdateRushSessionTx(ctx context.Context, tx pgx.Tx, session *PuzzleRushSession) error {
	query := `
		UPDATE puzzle_rush_sessions
		SET score = $1,
			strikes = $2,
			current_puzzle_id = $3,
			passed_puzzle_ids = $4,
			status = $5,
			ended_at = $6,
			moves_history = $7,
			updated_at = NOW()
		WHERE id = $8::uuid
	`

	if session.PassedPuzzleIDs == nil {
		session.PassedPuzzleIDs = []string{}
	}
	if session.MovesHistory == nil {
		session.MovesHistory = []RushPuzzleAttempt{}
	}

	movesJSON, err := json.Marshal(session.MovesHistory)
	if err != nil {
		movesJSON = []byte("[]")
	}

	_, err = tx.Exec(
		ctx,
		query,
		session.Score,
		session.Strikes,
		session.CurrentPuzzleID,
		session.PassedPuzzleIDs,
		session.Status,
		session.EndedAt,
		movesJSON,
		session.ID,
	)

	return err
}

func (r *Repository) FindRushPuzzle(ctx context.Context, minRating, maxRating int, excludedIDs []string, theme string) (*Puzzle, error) {
	if excludedIDs == nil {
		excludedIDs = []string{}
	}

	offset := rand.IntN(100)
	query := `
		WITH candidates AS (
			SELECT id, fen, moves, rating, rating_deviation, popularity, nb_plays, themes, COALESCE(game_url, ''), COALESCE(opening_tags, ''), created_at
			FROM puzzles
			WHERE rating BETWEEN $1 AND $2
			  AND NOT (id = ANY($3))
			  AND ($4 = '' OR $4 = ANY(themes))
			OFFSET $5
			LIMIT 50
		)
		SELECT * FROM candidates ORDER BY RANDOM() LIMIT 1
	`

	p := &Puzzle{}
	err := r.DB.QueryRow(ctx, query, minRating, maxRating, excludedIDs, theme, offset).Scan(
		&p.ID,
		&p.FEN,
		&p.Moves,
		&p.Rating,
		&p.RatingDeviation,
		&p.Popularity,
		&p.NbPlays,
		&p.Themes,
		&p.GameURL,
		&p.OpeningTags,
		&p.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) && offset > 0 {
		err = r.DB.QueryRow(ctx, query, minRating, maxRating, excludedIDs, theme, 0).Scan(
			&p.ID,
			&p.FEN,
			&p.Moves,
			&p.Rating,
			&p.RatingDeviation,
			&p.Popularity,
			&p.NbPlays,
			&p.Themes,
			&p.GameURL,
			&p.OpeningTags,
			&p.CreatedAt,
		)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		fallbackQuery := `
			WITH candidates AS (
				SELECT id, fen, moves, rating, rating_deviation, popularity, nb_plays, themes, COALESCE(game_url, ''), COALESCE(opening_tags, ''), created_at
				FROM puzzles
				WHERE rating BETWEEN $1 AND $2
				  AND ($3 = '' OR $3 = ANY(themes))
				LIMIT 50
			)
			SELECT * FROM candidates ORDER BY RANDOM() LIMIT 1
		`
		err = r.DB.QueryRow(ctx, fallbackQuery, minRating-150, maxRating+150, theme).Scan(
			&p.ID,
			&p.FEN,
			&p.Moves,
			&p.Rating,
			&p.RatingDeviation,
			&p.Popularity,
			&p.NbPlays,
			&p.Themes,
			&p.GameURL,
			&p.OpeningTags,
			&p.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return p, nil
}

func (r *Repository) GetOrCreateRushStats(ctx context.Context, userID int64) (*UserPuzzleRushStats, error) {
	query := `
		SELECT user_id, best_score_3min, best_score_5min, best_score_survival, total_runs, updated_at
		FROM user_puzzle_rush_stats
		WHERE user_id = $1
	`

	s := &UserPuzzleRushStats{}
	err := r.DB.QueryRow(ctx, query, userID).Scan(
		&s.UserID,
		&s.BestScore3Min,
		&s.BestScore5Min,
		&s.BestScoreSurvival,
		&s.TotalRuns,
		&s.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		insertQuery := `
			INSERT INTO user_puzzle_rush_stats (user_id, best_score_3min, best_score_5min, best_score_survival, total_runs, updated_at)
			VALUES ($1, 0, 0, 0, 0, NOW())
			ON CONFLICT (user_id) DO UPDATE SET updated_at = NOW()
			RETURNING user_id, best_score_3min, best_score_5min, best_score_survival, total_runs, updated_at
		`
		err = r.DB.QueryRow(ctx, insertQuery, userID).Scan(
			&s.UserID,
			&s.BestScore3Min,
			&s.BestScore5Min,
			&s.BestScoreSurvival,
			&s.TotalRuns,
			&s.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return s, nil
}

func (r *Repository) UpdateRushStats(ctx context.Context, stats *UserPuzzleRushStats) error {
	query := `
		UPDATE user_puzzle_rush_stats
		SET best_score_3min = $1,
			best_score_5min = $2,
			best_score_survival = $3,
			total_runs = $4,
			updated_at = NOW()
		WHERE user_id = $5
	`

	_, err := r.DB.Exec(
		ctx,
		query,
		stats.BestScore3Min,
		stats.BestScore5Min,
		stats.BestScoreSurvival,
		stats.TotalRuns,
		stats.UserID,
	)

	return err
}

func (r *Repository) GetOrCreateRushStatsForUpdate(ctx context.Context, tx pgx.Tx, userID int64) (*UserPuzzleRushStats, error) {
	query := `
		SELECT user_id, best_score_3min, best_score_5min, best_score_survival, total_runs, updated_at
		FROM user_puzzle_rush_stats
		WHERE user_id = $1
		FOR UPDATE
	`

	s := &UserPuzzleRushStats{}
	err := tx.QueryRow(ctx, query, userID).Scan(
		&s.UserID,
		&s.BestScore3Min,
		&s.BestScore5Min,
		&s.BestScoreSurvival,
		&s.TotalRuns,
		&s.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		insertQuery := `
			INSERT INTO user_puzzle_rush_stats (user_id, best_score_3min, best_score_5min, best_score_survival, total_runs, updated_at)
			VALUES ($1, 0, 0, 0, 0, NOW())
			ON CONFLICT (user_id) DO UPDATE SET updated_at = NOW()
			RETURNING user_id, best_score_3min, best_score_5min, best_score_survival, total_runs, updated_at
		`
		err = tx.QueryRow(ctx, insertQuery, userID).Scan(
			&s.UserID,
			&s.BestScore3Min,
			&s.BestScore5Min,
			&s.BestScoreSurvival,
			&s.TotalRuns,
			&s.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return s, nil
}

func (r *Repository) UpdateRushStatsTx(ctx context.Context, tx pgx.Tx, stats *UserPuzzleRushStats) error {
	query := `
		UPDATE user_puzzle_rush_stats
		SET best_score_3min = $1,
			best_score_5min = $2,
			best_score_survival = $3,
			total_runs = $4,
			updated_at = NOW()
		WHERE user_id = $5
	`

	_, err := tx.Exec(
		ctx,
		query,
		stats.BestScore3Min,
		stats.BestScore5Min,
		stats.BestScoreSurvival,
		stats.TotalRuns,
		stats.UserID,
	)

	return err
}

func (r *Repository) GetRushLeaderboard(ctx context.Context, mode string, limit int) ([]RushLeaderboardEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
		SELECT s.user_id, u.username, MAX(s.score) AS high_score, s.mode, MAX(s.started_at) AS played_at
		FROM puzzle_rush_sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.mode = $1 AND s.score > 0
		GROUP BY s.user_id, u.username, s.mode
		ORDER BY high_score DESC
		LIMIT $2
	`

	rows, err := r.DB.Query(ctx, query, mode, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]RushLeaderboardEntry, 0)
	for rows.Next() {
		var e RushLeaderboardEntry
		if err := rows.Scan(
			&e.UserID,
			&e.Username,
			&e.Score,
			&e.Mode,
			&e.PlayedAt,
		); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func (r *Repository) GetUserRushHistory(ctx context.Context, userID int64, mode string, limit, offset int) (*RushHistoryResponse, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	countQuery := `
		SELECT COUNT(*)
		FROM puzzle_rush_sessions
		WHERE user_id = $1 AND ($2 = '' OR mode = $2)
	`
	var total int
	if err := r.DB.QueryRow(ctx, countQuery, userID, mode).Scan(&total); err != nil {
		return nil, err
	}

	query := `
		SELECT id, mode, theme, score, strikes, status, started_at, ended_at, COALESCE(moves_history, '[]'::jsonb)
		FROM puzzle_rush_sessions
		WHERE user_id = $1 AND ($2 = '' OR mode = $2)
		ORDER BY started_at DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := r.DB.Query(ctx, query, userID, mode, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]RushHistoryItem, 0)
	for rows.Next() {
		var item RushHistoryItem
		var movesData []byte
		if err := rows.Scan(
			&item.SessionID,
			&item.Mode,
			&item.Theme,
			&item.Score,
			&item.Strikes,
			&item.Status,
			&item.StartedAt,
			&item.EndedAt,
			&movesData,
		); err != nil {
			return nil, err
		}

		if len(movesData) > 0 {
			_ = json.Unmarshal(movesData, &item.MovesHistory)
		}
		if item.MovesHistory == nil {
			item.MovesHistory = []RushPuzzleAttempt{}
		}
		item.TotalPuzzlesFaced = len(item.MovesHistory)

		ended := time.Now().UTC()
		if item.EndedAt != nil {
			ended = *item.EndedAt
		}
		item.DurationSec = int(ended.Sub(item.StartedAt).Seconds())
		if item.DurationSec < 0 {
			item.DurationSec = 0
		}

		runs = append(runs, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &RushHistoryResponse{
		Total: total,
		Runs:  runs,
	}, nil
}

func (r *Repository) GetThemes(ctx context.Context) ([]PuzzleThemeInfo, error) {
	if r.Redis != nil {
		cachedData, err := r.Redis.Get(ctx, "puzzle:themes").Bytes()
		if err == nil && len(cachedData) > 0 {
			var cachedThemes []PuzzleThemeInfo
			if err := json.Unmarshal(cachedData, &cachedThemes); err == nil && len(cachedThemes) > 0 {
				return cachedThemes, nil
			}
		}
	}

	r.themesMu.RLock()
	if time.Now().Before(r.themesExpire) && len(r.themesCache) > 0 {
		cached := make([]PuzzleThemeInfo, len(r.themesCache))
		copy(cached, r.themesCache)
		r.themesMu.RUnlock()
		return cached, nil
	}
	r.themesMu.RUnlock()

	r.themesMu.Lock()
	defer r.themesMu.Unlock()

	if time.Now().Before(r.themesExpire) && len(r.themesCache) > 0 {
		cached := make([]PuzzleThemeInfo, len(r.themesCache))
		copy(cached, r.themesCache)
		return cached, nil
	}

	query := `
		SELECT unnest(themes) AS theme, COUNT(*) AS count
		FROM puzzles
		GROUP BY theme
		HAVING COUNT(*) >= 50
		ORDER BY count DESC
	`

	rows, err := r.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	themes := make([]PuzzleThemeInfo, 0)
	for rows.Next() {
		var t PuzzleThemeInfo
		if err := rows.Scan(&t.ID, &t.Count); err != nil {
			return nil, err
		}
		t.Name = formatThemeName(t.ID)
		themes = append(themes, t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	r.themesCache = themes
	r.themesExpire = time.Now().Add(1 * time.Hour)

	if r.Redis != nil {
		if data, err := json.Marshal(themes); err == nil {
			_ = r.Redis.Set(ctx, "puzzle:themes", data, 24*time.Hour).Err()
		}
	}

	return themes, nil
}

func (r *Repository) FindCustomPuzzle(ctx context.Context, filter CustomPuzzleFilter) (*Puzzle, error) {
	if filter.Themes == nil {
		filter.Themes = []string{}
	}

	if filter.ReviewMistakes {
		query := `
			WITH candidates AS (
				SELECT p.id, p.fen, p.moves, p.rating, p.rating_deviation, p.popularity, p.nb_plays, p.themes, COALESCE(p.game_url, ''), COALESCE(p.opening_tags, ''), p.created_at
				FROM puzzles p
				JOIN user_puzzle_attempts a ON p.id = a.puzzle_id
				WHERE a.user_id = $1
				  AND a.solved = false
				  AND ($2::INT <= 0 OR p.rating >= $2)
				  AND ($3::INT <= 0 OR p.rating <= $3)
				  AND ($4::TEXT[] IS NULL OR cardinality($4) = 0 OR p.themes && $4)
				LIMIT 50
			)
			SELECT * FROM candidates ORDER BY RANDOM() LIMIT 1
		`
		p := &Puzzle{}
		err := r.DB.QueryRow(ctx, query, filter.UserID, filter.MinRating, filter.MaxRating, filter.Themes).Scan(
			&p.ID,
			&p.FEN,
			&p.Moves,
			&p.Rating,
			&p.RatingDeviation,
			&p.Popularity,
			&p.NbPlays,
			&p.Themes,
			&p.GameURL,
			&p.OpeningTags,
			&p.CreatedAt,
		)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}

	offset := rand.IntN(100)
	query := `
		WITH candidates AS (
			SELECT id, fen, moves, rating, rating_deviation, popularity, nb_plays, themes, COALESCE(game_url, ''), COALESCE(opening_tags, ''), created_at
			FROM puzzles
			WHERE ($1::INT <= 0 OR rating >= $1)
			  AND ($2::INT <= 0 OR rating <= $2)
			  AND ($3::TEXT[] IS NULL OR cardinality($3) = 0 OR themes && $3)
			  AND (
			    NOT $4::BOOLEAN OR id NOT IN (
			      SELECT puzzle_id FROM user_puzzle_attempts WHERE user_id = $5 AND solved = true
			    )
			  )
			OFFSET $6
			LIMIT 50
		)
		SELECT * FROM candidates ORDER BY RANDOM() LIMIT 1
	`

	p := &Puzzle{}
	err := r.DB.QueryRow(ctx, query, filter.MinRating, filter.MaxRating, filter.Themes, filter.ExcludeSolved, filter.UserID, offset).Scan(
		&p.ID,
		&p.FEN,
		&p.Moves,
		&p.Rating,
		&p.RatingDeviation,
		&p.Popularity,
		&p.NbPlays,
		&p.Themes,
		&p.GameURL,
		&p.OpeningTags,
		&p.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) && offset > 0 {
		err = r.DB.QueryRow(ctx, query, filter.MinRating, filter.MaxRating, filter.Themes, filter.ExcludeSolved, filter.UserID, 0).Scan(
			&p.ID,
			&p.FEN,
			&p.Moves,
			&p.Rating,
			&p.RatingDeviation,
			&p.Popularity,
			&p.NbPlays,
			&p.Themes,
			&p.GameURL,
			&p.OpeningTags,
			&p.CreatedAt,
		)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		fallbackQuery := `
			WITH candidates AS (
				SELECT id, fen, moves, rating, rating_deviation, popularity, nb_plays, themes, COALESCE(game_url, ''), COALESCE(opening_tags, ''), created_at
				FROM puzzles
				WHERE ($1::TEXT[] IS NULL OR cardinality($1) = 0 OR themes && $1)
				LIMIT 50
			)
			SELECT * FROM candidates ORDER BY RANDOM() LIMIT 1
		`
		err = r.DB.QueryRow(ctx, fallbackQuery, filter.Themes).Scan(
			&p.ID,
			&p.FEN,
			&p.Moves,
			&p.Rating,
			&p.RatingDeviation,
			&p.Popularity,
			&p.NbPlays,
			&p.Themes,
			&p.GameURL,
			&p.OpeningTags,
			&p.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return p, nil
}

func formatThemeName(theme string) string {
	names := map[string]string{
		"fork":              "Fork",
		"pin":               "Pin",
		"skewer":            "Skewer",
		"discoveredAttack":  "Discovered Attack",
		"doubleCheck":       "Double Check",
		"attraction":        "Attraction",
		"deflection":        "Deflection",
		"clearance":         "Clearance",
		"interference":      "Interference",
		"xRayAttack":        "X-Ray Attack",
		"hangingPiece":      "Hanging Piece",
		"trappedPiece":      "Trapped Piece",
		"mate":              "Checkmate",
		"mateIn1":           "Mate in 1",
		"mateIn2":           "Mate in 2",
		"mateIn3":           "Mate in 3",
		"mateIn4":           "Mate in 4",
		"mateIn5":           "Mate in 5",
		"backRankMate":      "Back-Rank Mate",
		"smotheredMate":     "Smothered Mate",
		"anastasiaMate":     "Anastasia's Mate",
		"arabianMate":       "Arabian Mate",
		"bodenMate":         "Boden's Mate",
		"hookMate":          "Hook Mate",
		"dovetailMate":      "Dovetail Mate",
		"opening":           "Opening",
		"middlegame":        "Middlegame",
		"endgame":           "Endgame",
		"pawnEndgame":       "Pawn Endgame",
		"knightEndgame":     "Knight Endgame",
		"bishopEndgame":     "Bishop Endgame",
		"rookEndgame":       "Rook Endgame",
		"queenEndgame":      "Queen Endgame",
		"queenRookEndgame":  "Queen and Rook Endgame",
		"crushing":          "Crushing Advantage",
		"advantage":         "Advantage",
		"equality":          "Equality",
		"zugzwang":          "Zugzwang",
		"defensiveMove":     "Defensive Move",
		"quietMove":         "Quiet Move",
		"sacrifice":         "Sacrifice",
		"intermezzo":        "Zwischenzug / In-Between",
		"promotion":         "Pawn Promotion",
		"underPromotion":    "Underpromotion",
		"capturingDefender": "Capturing the Defender",
		"exposedKing":       "Exposed King",
		"kingsideAttack":    "Kingside Attack",
		"queensideAttack":   "Queenside Attack",
		"short":             "Short Tactic",
		"long":              "Long Combination",
		"veryLong":          "Very Long Combination",
		"oneMove":           "One-Move Tactic",
	}
	if name, ok := names[theme]; ok {
		return name
	}
	return theme
}
