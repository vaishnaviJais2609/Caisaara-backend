package puzzle

import (
	"time"
)

type Puzzle struct {
	ID              string    `json:"id"`
	FEN             string    `json:"fen"`
	Moves           string    `json:"moves"`
	Rating          int       `json:"rating"`
	RatingDeviation int       `json:"rating_deviation"`
	Popularity      int       `json:"popularity"`
	NbPlays         int       `json:"nb_plays"`
	Themes          []string  `json:"themes"`
	GameURL         string    `json:"game_url,omitempty"`
	OpeningTags     string    `json:"opening_tags,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type UserPuzzleRating struct {
	UserID           int64     `json:"user_id"`
	Rating           int       `json:"rating"`
	RatingDeviation  float64   `json:"rating_deviation"`
	RatingVolatility float64   `json:"rating_volatility"`
	PuzzlesSolved    int       `json:"puzzles_solved"`
	PuzzlesFailed    int       `json:"puzzles_failed"`
	BestRating       int       `json:"best_rating"`
	BestRatingAt     time.Time `json:"best_rating_at"`
	CurrentStreak    int       `json:"current_streak"`
	MaxStreak        int       `json:"max_streak"`
	LastPlayedDate   string    `json:"last_played_date,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type UserPuzzleAttempt struct {
	ID               int64     `json:"id"`
	UserID           int64     `json:"user_id"`
	PuzzleID         string    `json:"puzzle_id"`
	Solved           bool      `json:"solved"`
	UserRatingBefore int       `json:"user_rating_before"`
	UserRatingAfter  int       `json:"user_rating_after"`
	TimeTakenMs      int       `json:"time_taken_ms"`
	AttemptedMoves   []string  `json:"attempted_moves"`
	CreatedAt        time.Time `json:"created_at"`
}

type DailyPuzzleSession struct {
	UserID         int64    `json:"user_id"`
	PuzzleID       string   `json:"puzzle_id"`
	Date           string   `json:"date"`
	AttemptsLeft   int      `json:"attempts_left"`
	HintUsed       bool     `json:"hint_used"`
	SolutionViewed bool     `json:"solution_viewed"`
	Solved         bool     `json:"solved"`
	RatingEarned   int      `json:"rating_earned"`
	AttemptedMoves []string `json:"attempted_moves"`
}

type DailyPuzzleResponse struct {
	PuzzleID       string   `json:"puzzle_id"`
	FEN            string   `json:"fen"`
	InitialMove    string   `json:"initial_move"`
	Rating         int      `json:"rating"`
	Themes         []string `json:"themes"`
	AttemptsLeft   int      `json:"attempts_left"`
	HintAvailable  bool     `json:"hint_available"`
	SolutionViewed bool     `json:"solution_viewed"`
	Solved         bool     `json:"solved"`
	CurrentStreak  int      `json:"current_streak"`
	PlayerRating   int      `json:"player_rating"`
	AttemptedMoves []string `json:"attempted_moves"`
}

type SubmitAttemptRequest struct {
	PuzzleID    string `json:"puzzle_id" binding:"required"`
	Move        string `json:"move" binding:"required"`
	TimeTakenMs int    `json:"time_taken_ms"`
}

type SubmitAttemptResponse struct {
	Correct          bool     `json:"correct"`
	Solved           bool     `json:"solved"`
	NextMove         string   `json:"next_move,omitempty"`
	AttemptsLeft     int      `json:"attempts_left"`
	RatingChange     int      `json:"rating_change"`
	UserRatingBefore int      `json:"user_rating_before"`
	UserRatingAfter  int      `json:"user_rating_after"`
	CurrentStreak    int      `json:"current_streak"`
	AttemptedMoves   []string `json:"attempted_moves"`
	Message          string   `json:"message"`
}

type HintRequest struct {
	PuzzleID string `json:"puzzle_id"`
}

type HintResponse struct {
	HintType   string `json:"hint_type"`
	FromSquare string `json:"from_square"`
	PieceType  string `json:"piece_type,omitempty"`
	Message    string `json:"message"`
	HintUsed   bool   `json:"hint_used"`
}

type SolutionRequest struct {
	PuzzleID string `json:"puzzle_id"`
}

type SolutionResponse struct {
	Moves          []string `json:"moves"`
	RatingChange   int      `json:"rating_change"`
	SolutionViewed bool     `json:"solution_viewed"`
	Message        string   `json:"message"`
}

type PuzzleStatsResponse struct {
	Rating        int        `json:"rating"`
	BestRating    int        `json:"best_rating"`
	BestRatingAt  *time.Time `json:"best_rating_at,omitempty"`
	PuzzlesSolved int        `json:"puzzles_solved"`
	PuzzlesFailed int        `json:"puzzles_failed"`
	WinRate       float64    `json:"win_rate"`
	CurrentStreak int        `json:"current_streak"`
	MaxStreak     int        `json:"max_streak"`
}

type PuzzleHistoryItem struct {
	ID               int64     `json:"id"`
	PuzzleID         string    `json:"puzzle_id"`
	Solved           bool      `json:"solved"`
	AttemptedMoves   []string  `json:"attempted_moves"`
	SolutionMoves    []string  `json:"solution_moves"`
	UserRatingBefore int       `json:"user_rating_before"`
	UserRatingAfter  int       `json:"user_rating_after"`
	RatingChange     int       `json:"rating_change"`
	TimeTakenMs      int       `json:"time_taken_ms"`
	CreatedAt        time.Time `json:"created_at"`
}

type PuzzleHistoryResponse struct {
	Total    int64               `json:"total"`
	Attempts []PuzzleHistoryItem `json:"attempts"`
}

type RushPuzzleAttempt struct {
	PuzzleID       string   `json:"puzzle_id"`
	Solved         bool     `json:"solved"`
	AttemptedMoves []string `json:"attempted_moves"`
	SolutionMoves  []string `json:"solution_moves"`
	Rating         int      `json:"rating"`
}

type PuzzleRushSession struct {
	ID              string              `json:"id"`
	UserID          int64               `json:"user_id"`
	Mode            string              `json:"mode"`
	Score           int                 `json:"score"`
	Strikes         int                 `json:"strikes"`
	CurrentPuzzleID string              `json:"current_puzzle_id"`
	PassedPuzzleIDs []string            `json:"passed_puzzle_ids"`
	Status          string              `json:"status"`
	TimeLimitSec    int                 `json:"time_limit_sec"`
	Theme           string              `json:"theme,omitempty"`
	MovesHistory    []RushPuzzleAttempt `json:"moves_history"`
	StartedAt       time.Time           `json:"started_at"`
	EndedAt         *time.Time          `json:"ended_at,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

type UserPuzzleRushStats struct {
	UserID            int64     `json:"user_id"`
	BestScore3Min     int       `json:"best_score_3min"`
	BestScore5Min     int       `json:"best_score_5min"`
	BestScoreSurvival int       `json:"best_score_survival"`
	TotalRuns         int       `json:"total_runs"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type StartRushRequest struct {
	Mode  string `json:"mode" binding:"required"`
	Theme string `json:"theme,omitempty"`
}

type RushPuzzleData struct {
	PuzzleID    string   `json:"puzzle_id"`
	FEN         string   `json:"fen"`
	InitialMove string   `json:"initial_move"`
	Rating      int      `json:"rating"`
	Themes      []string `json:"themes"`
}

type StartRushResponse struct {
	SessionID    string         `json:"session_id"`
	Mode         string         `json:"mode"`
	TimeLimitSec int            `json:"time_limit_sec"`
	Theme        string         `json:"theme,omitempty"`
	Score        int            `json:"score"`
	Strikes      int            `json:"strikes"`
	StrikesLeft  int            `json:"strikes_left"`
	Puzzle       RushPuzzleData `json:"puzzle"`
}

type SubmitRushMoveRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	PuzzleID  string `json:"puzzle_id" binding:"required"`
	Move      string `json:"move" binding:"required"`
}

type RushSummary struct {
	SessionID      string `json:"session_id"`
	Mode           string `json:"mode"`
	FinalScore     int    `json:"final_score"`
	TotalStrikes   int    `json:"total_strikes"`
	IsNewHighScore bool   `json:"is_new_high_score"`
	BestScore      int    `json:"best_score"`
	DurationSec    int    `json:"duration_sec"`
}

type SubmitRushMoveResponse struct {
	Correct     bool            `json:"correct"`
	Score       int             `json:"score"`
	Strikes     int             `json:"strikes"`
	StrikesLeft int             `json:"strikes_left"`
	GameOver    bool            `json:"game_over"`
	Reason      string          `json:"reason,omitempty"`
	NextPuzzle  *RushPuzzleData `json:"next_puzzle,omitempty"`
	Summary     *RushSummary    `json:"summary,omitempty"`
}

type EndRushRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

type RushLeaderboardEntry struct {
	UserID   int64     `json:"user_id"`
	Username string    `json:"username"`
	Score    int       `json:"score"`
	Mode     string    `json:"mode"`
	PlayedAt time.Time `json:"played_at"`
}

type RushLeaderboardResponse struct {
	Mode    string                 `json:"mode"`
	Entries []RushLeaderboardEntry `json:"entries"`
}

type UserRushStatsResponse struct {
	BestScore3Min     int `json:"best_score_3min"`
	BestScore5Min     int `json:"best_score_5min"`
	BestScoreSurvival int `json:"best_score_survival"`
	TotalRuns         int `json:"total_runs"`
}

type RushHistoryItem struct {
	SessionID         string              `json:"session_id"`
	Mode              string              `json:"mode"`
	Theme             string              `json:"theme,omitempty"`
	Score             int                 `json:"score"`
	Strikes           int                 `json:"strikes"`
	Status            string              `json:"status"`
	DurationSec       int                 `json:"duration_sec"`
	TotalPuzzlesFaced int                 `json:"total_puzzles_faced"`
	MovesHistory      []RushPuzzleAttempt `json:"moves_history"`
	StartedAt         time.Time           `json:"started_at"`
	EndedAt           *time.Time          `json:"ended_at,omitempty"`
}

type RushHistoryResponse struct {
	Total int               `json:"total"`
	Runs  []RushHistoryItem `json:"runs"`
}

type PuzzleThemeInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type ThemesResponse struct {
	Themes []PuzzleThemeInfo `json:"themes"`
}

type CustomPuzzleFilter struct {
	UserID         int64
	Themes         []string
	MinRating      int
	MaxRating      int
	ExcludeSolved  bool
	ReviewMistakes bool
}

type CustomPuzzleResponse struct {
	PuzzleID    string   `json:"puzzle_id"`
	FEN         string   `json:"fen"`
	InitialMove string   `json:"initial_move"`
	Rating      int      `json:"rating"`
	Themes      []string `json:"themes"`
}

type SubmitCustomAttemptRequest struct {
	PuzzleID    string `json:"puzzle_id" binding:"required"`
	Move        string `json:"move" binding:"required"`
	TimeTakenMs int    `json:"time_taken_ms"`
	Rated       bool   `json:"rated"`
}

type SubmitCustomAttemptResponse struct {
	Correct          bool     `json:"correct"`
	Solved           bool     `json:"solved"`
	PuzzleID         string   `json:"puzzle_id"`
	AttemptedMoves   []string `json:"attempted_moves"`
	SolutionMoves    []string `json:"solution_moves"`
	RatingChange     int      `json:"rating_change"`
	UserRatingBefore int      `json:"user_rating_before"`
	UserRatingAfter  int      `json:"user_rating_after"`
	Rated            bool     `json:"rated"`
}
