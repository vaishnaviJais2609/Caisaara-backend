package tournament

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/here-arjun-1/Caisaara-backend/internal/game"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TournamentWithPlayerCount struct {
	Tournament
	Players int
}

type TournamentRepository interface {
	CreateTournament(ctx context.Context, tournament *Tournament) error
	FindByID(ctx context.Context, id int64) (*Tournament, error)
	ListPublicTournaments(ctx context.Context, limit int, offset int) ([]*TournamentWithPlayerCount, error)
	FindByIDWithPlayerCount(ctx context.Context, id int64) (*TournamentWithPlayerCount, error)
	FindByInviteCode(ctx context.Context, code string) (*TournamentWithPlayerCount, error)
	UpdateInviteCode(ctx context.Context, tournamentID int64, newCode string, expiresAt time.Time) error
	IsPlayerJoined(ctx context.Context, tournamentID int64, userID int64) (bool, error)
	AddPlayer(ctx context.Context, player *TournamentPlayer) error
	RemovePlayer(ctx context.Context, tournamentID int64, userID int64) error
	StartTournament(ctx context.Context, tournamentID int64) error
	UndoStartTournament(ctx context.Context, tournamentID int64) error
	EndTournament(ctx context.Context, tournamentID int64) error
	CancelTournament(ctx context.Context, tournamentID int64) ([]string, error)
	GetSwissPlayers(ctx context.Context, tournamentID int64) ([]*SwissPlayer, error)
	SaveRoundPairings(ctx context.Context, tournamentID int64, roundNumber int, pairings []SwissPairingResult, gameRepo ...game.GameRepository) ([]*TournamentPairing, error)
	UpdatePairingOnGameCompleted(ctx context.Context, gameID string, result string) (int64, bool, error)
	GetStandings(ctx context.Context, tournamentID int64) ([]*StandingsPlayerResponse, error)
	GetRounds(ctx context.Context, tournamentID int64, roundNumber ...int) ([]*RoundResponse, error)
	GetGames(ctx context.Context, tournamentID int64, userID ...int64) ([]*TournamentGameItem, error)
	UpdateTotalRounds(ctx context.Context, tournamentID int64, totalRounds int) error
	GetRoundWinners(ctx context.Context, tournamentID int64, roundID int64) ([]int64, error)
	DeleteOldTournaments(ctx context.Context, olderThan time.Duration) (int64, error)
	GetPlayerRating(ctx context.Context, userID int64, mode string) (int, error)
	RemoveUserFromTournaments(ctx context.Context, userID int64) ([]int64, error)
	GetWithdrawnPlayers(ctx context.Context, tournamentID int64) (map[int64]bool, error)
	FinishRoundIfNoGames(ctx context.Context, tournamentID int64, roundNumber int) (bool, error)
}

type Repository struct {
	DB *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) TournamentRepository {
	return &Repository{
		DB: db,
	}
}

func parseTimeControl(tcStr string) (int, int, bool) {
	tcStr = strings.ToLower(strings.TrimSpace(tcStr))
	if strings.HasSuffix(tcStr, "d") || strings.Contains(tcStr, "day") {
		fields := strings.Fields(tcStr)
		if len(fields) > 0 {
			valStr := strings.TrimSuffix(fields[0], "d")
			if val, err := strconv.Atoi(valStr); err == nil && val > 0 {
				return val * 1440, 0, true
			}
		}
		return 1440, 0, true
	}

	sep := "+"
	if strings.Contains(tcStr, "|") {
		sep = "|"
	}
	if strings.Contains(tcStr, sep) {
		parts := strings.Split(tcStr, sep)
		minVal, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		incVal, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 == nil && err2 == nil && minVal >= 0 && incVal >= 0 && minVal+incVal > 0 {
			return minVal, incVal, true
		}
		return 5, 0, false
	}

	tcClean := strings.TrimSuffix(strings.TrimSuffix(tcStr, "min"), "m")
	if val, err := strconv.Atoi(strings.TrimSpace(tcClean)); err == nil && val > 0 {
		return val, 0, true
	}

	return 5, 0, false
}

func (r *Repository) CreateTournament(ctx context.Context, tournament *Tournament) error {
	err := r.DB.QueryRow(
		ctx,
		`INSERT INTO tournaments (
			name,
			description,
			format,
			time_control,
			min_players,
			max_players,
			visibility,
			invite_code,
			invite_code_expires_at,
			status,
			total_rounds,
			current_round,
			created_by,
			start_at,
			created_at,
			updated_at,
			min_rating
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING id, created_at, updated_at`,
		tournament.Name,
		tournament.Description,
		tournament.Format,
		tournament.TimeControl,
		tournament.MinPlayers,
		tournament.MaxPlayers,
		tournament.Visibility,
		tournament.InviteCode,
		tournament.InviteCodeExpiresAt,
		tournament.Status,
		tournament.TotalRounds,
		tournament.CurrentRound,
		tournament.CreatedBy,
		tournament.StartAt,
		tournament.CreatedAt,
		tournament.UpdatedAt,
		tournament.MinRating,
	).Scan(&tournament.ID, &tournament.CreatedAt, &tournament.UpdatedAt)

	if err != nil {
		slog.ErrorContext(ctx, "create tournament repository failed", "error", err)
		return err
	}

	return nil
}

func (r *Repository) FindByID(ctx context.Context, id int64) (*Tournament, error) {
	row := r.DB.QueryRow(
		ctx,
		`SELECT
			id,
			name,
			COALESCE(description, ''),
			format,
			time_control,
			min_players,
			max_players,
			visibility,
			invite_code,
			invite_code_expires_at,
			status,
			total_rounds,
			current_round,
			created_by,
			start_at,
			created_at,
			updated_at,
			min_rating
		FROM tournaments
		WHERE id = $1`,
		id,
	)

	var t Tournament
	err := row.Scan(
		&t.ID,
		&t.Name,
		&t.Description,
		&t.Format,
		&t.TimeControl,
		&t.MinPlayers,
		&t.MaxPlayers,
		&t.Visibility,
		&t.InviteCode,
		&t.InviteCodeExpiresAt,
		&t.Status,
		&t.TotalRounds,
		&t.CurrentRound,
		&t.CreatedBy,
		&t.StartAt,
		&t.CreatedAt,
		&t.UpdatedAt,
		&t.MinRating,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTournamentNotFound
		}
		slog.ErrorContext(ctx, "find tournament by id failed", "id", id, "error", err)
		return nil, err
	}

	return &t, nil
}

func (r *Repository) ListPublicTournaments(ctx context.Context, limit int, offset int) ([]*TournamentWithPlayerCount, error) {
	rows, err := r.DB.Query(
		ctx,
		`SELECT
			t.id,
			t.name,
			COALESCE(t.description, ''),
			t.format,
			t.time_control,
			t.min_players,
			t.max_players,
			t.visibility,
			t.invite_code,
			t.invite_code_expires_at,
			t.status,
			t.total_rounds,
			t.current_round,
			t.created_by,
			t.start_at,
			t.created_at,
			t.updated_at,
			t.min_rating,
			COUNT(tp.id)::int AS players
		FROM tournaments t
		LEFT JOIN tournament_players tp ON t.id = tp.tournament_id
		WHERE t.visibility = 'public'
		GROUP BY t.id
		ORDER BY t.created_at DESC
		LIMIT $1 OFFSET $2`,
		limit,
		offset,
	)
	if err != nil {
		slog.ErrorContext(ctx, "list public tournaments repository failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	list := make([]*TournamentWithPlayerCount, 0)
	for rows.Next() {
		var tw TournamentWithPlayerCount
		err := rows.Scan(
			&tw.ID,
			&tw.Name,
			&tw.Description,
			&tw.Format,
			&tw.TimeControl,
			&tw.MinPlayers,
			&tw.MaxPlayers,
			&tw.Visibility,
			&tw.InviteCode,
			&tw.InviteCodeExpiresAt,
			&tw.Status,
			&tw.TotalRounds,
			&tw.CurrentRound,
			&tw.CreatedBy,
			&tw.StartAt,
			&tw.CreatedAt,
			&tw.UpdatedAt,
			&tw.MinRating,
			&tw.Players,
		)
		if err != nil {
			slog.ErrorContext(ctx, "scan tournament with player count failed", "error", err)
			return nil, err
		}
		list = append(list, &tw)
	}

	return list, nil
}

func (r *Repository) FindByIDWithPlayerCount(ctx context.Context, id int64) (*TournamentWithPlayerCount, error) {
	row := r.DB.QueryRow(
		ctx,
		`SELECT
			t.id,
			t.name,
			COALESCE(t.description, ''),
			t.format,
			t.time_control,
			t.min_players,
			t.max_players,
			t.visibility,
			t.invite_code,
			t.invite_code_expires_at,
			t.status,
			t.total_rounds,
			t.current_round,
			t.created_by,
			t.start_at,
			t.created_at,
			t.updated_at,
			t.min_rating,
			COUNT(tp.id)::int AS players
		FROM tournaments t
		LEFT JOIN tournament_players tp ON t.id = tp.tournament_id
		WHERE t.id = $1
		GROUP BY t.id`,
		id,
	)

	var tw TournamentWithPlayerCount
	err := row.Scan(
		&tw.ID,
		&tw.Name,
		&tw.Description,
		&tw.Format,
		&tw.TimeControl,
		&tw.MinPlayers,
		&tw.MaxPlayers,
		&tw.Visibility,
		&tw.InviteCode,
		&tw.InviteCodeExpiresAt,
		&tw.Status,
		&tw.TotalRounds,
		&tw.CurrentRound,
		&tw.CreatedBy,
		&tw.StartAt,
		&tw.CreatedAt,
		&tw.UpdatedAt,
		&tw.MinRating,
		&tw.Players,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTournamentNotFound
		}
		slog.ErrorContext(ctx, "find tournament by id with player count failed", "id", id, "error", err)
		return nil, err
	}

	return &tw, nil
}

func (r *Repository) FindByInviteCode(ctx context.Context, code string) (*TournamentWithPlayerCount, error) {
	row := r.DB.QueryRow(
		ctx,
		`SELECT
			t.id,
			t.name,
			COALESCE(t.description, ''),
			t.format,
			t.time_control,
			t.min_players,
			t.max_players,
			t.visibility,
			t.invite_code,
			t.invite_code_expires_at,
			t.status,
			t.total_rounds,
			t.current_round,
			t.created_by,
			t.start_at,
			t.created_at,
			t.updated_at,
			t.min_rating,
			COUNT(tp.id)::int AS players
		FROM tournaments t
		LEFT JOIN tournament_players tp ON t.id = tp.tournament_id
		WHERE LOWER(t.invite_code) = LOWER($1)
		GROUP BY t.id`,
		code,
	)

	var tw TournamentWithPlayerCount
	err := row.Scan(
		&tw.ID,
		&tw.Name,
		&tw.Description,
		&tw.Format,
		&tw.TimeControl,
		&tw.MinPlayers,
		&tw.MaxPlayers,
		&tw.Visibility,
		&tw.InviteCode,
		&tw.InviteCodeExpiresAt,
		&tw.Status,
		&tw.TotalRounds,
		&tw.CurrentRound,
		&tw.CreatedBy,
		&tw.StartAt,
		&tw.CreatedAt,
		&tw.UpdatedAt,
		&tw.MinRating,
		&tw.Players,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTournamentNotFound
		}
		slog.ErrorContext(ctx, "find tournament by invite code failed", "code", code, "error", err)
		return nil, err
	}

	return &tw, nil
}

func (r *Repository) IsPlayerJoined(ctx context.Context, tournamentID int64, userID int64) (bool, error) {
	var exists bool
	err := r.DB.QueryRow(
		ctx,
		`SELECT EXISTS (
			SELECT 1 FROM tournament_players
			WHERE tournament_id = $1 AND user_id = $2
		)`,
		tournamentID,
		userID,
	).Scan(&exists)
	if err != nil {
		slog.ErrorContext(ctx, "check is player joined failed", "tournament_id", tournamentID, "user_id", userID, "error", err)
		return false, err
	}
	return exists, nil
}

func (r *Repository) GetPlayerRating(ctx context.Context, userID int64, mode string) (int, error) {
	var rating int
	err := r.DB.QueryRow(
		ctx,
		`SELECT COALESCE(pr.rating, u.start_rating, 1200)
		FROM users u
		LEFT JOIN player_ratings pr ON pr.user_id = u.id AND pr.mode = $2
		WHERE u.id = $1`,
		userID,
		mode,
	).Scan(&rating)
	if err != nil {
		slog.ErrorContext(ctx, "get player rating failed", "user_id", userID, "mode", mode, "error", err)
		return 0, err
	}
	return rating, nil
}

func (r *Repository) AddPlayer(ctx context.Context, player *TournamentPlayer) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	var maxPlayers int
	err = tx.QueryRow(
		ctx,
		`SELECT status, max_players FROM tournaments WHERE id = $1 FOR UPDATE`,
		player.TournamentID,
	).Scan(&status, &maxPlayers)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTournamentNotFound
		}
		return err
	}
	if status != StatusRegistration {
		return ErrNotRegistration
	}

	var playerCount int
	err = tx.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM tournament_players WHERE tournament_id = $1`,
		player.TournamentID,
	).Scan(&playerCount)
	if err != nil {
		return err
	}
	if playerCount >= maxPlayers {
		return ErrTournamentFull
	}

	err = tx.QueryRow(
		ctx,
		`INSERT INTO tournament_players (
			tournament_id,
			user_id,
			score,
			wins,
			draws,
			losses,
			games_played,
			joined_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, joined_at`,
		player.TournamentID,
		player.UserID,
		player.Score,
		player.Wins,
		player.Draws,
		player.Losses,
		player.GamesPlayed,
		player.JoinedAt,
	).Scan(&player.ID, &player.JoinedAt)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrAlreadyJoined
		}
		slog.ErrorContext(ctx, "add tournament player failed", "tournament_id", player.TournamentID, "user_id", player.UserID, "error", err)
		return err
	}

	return tx.Commit(ctx)
}

func (r *Repository) RemovePlayer(ctx context.Context, tournamentID int64, userID int64) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM tournaments WHERE id = $1 FOR UPDATE`, tournamentID).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTournamentNotFound
		}
		return err
	}
	if status != StatusRegistration {
		return ErrCannotLeaveStarted
	}

	tag, err := tx.Exec(
		ctx,
		`DELETE FROM tournament_players
		WHERE tournament_id = $1 AND user_id = $2`,
		tournamentID,
		userID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "remove tournament player failed", "tournament_id", tournamentID, "user_id", userID, "error", err)
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotJoined
	}
	return tx.Commit(ctx)
}

func (r *Repository) StartTournament(ctx context.Context, tournamentID int64) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "begin transaction failed for start tournament", "tournament_id", tournamentID, "error", err)
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(
		ctx,
		`UPDATE tournaments
		SET status = 'ongoing', current_round = 1, updated_at = NOW()
		WHERE id = $1 AND status = 'registration'`,
		tournamentID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "update tournament status to ongoing failed", "tournament_id", tournamentID, "error", err)
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotRegistration
	}

	_, err = tx.Exec(
		ctx,
		`INSERT INTO tournament_rounds (tournament_id, round_number, status, started_at)
		VALUES ($1, 1, 'ongoing', NOW())
		ON CONFLICT (tournament_id, round_number)
		DO UPDATE SET status = 'ongoing', started_at = NOW()`,
		tournamentID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "insert round 1 failed", "tournament_id", tournamentID, "error", err)
		return err
	}

	return tx.Commit(ctx)
}

func (r *Repository) EndTournament(ctx context.Context, tournamentID int64) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "begin transaction failed for end tournament", "tournament_id", tournamentID, "error", err)
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(
		ctx,
		`UPDATE tournament_pairings
		SET status = 'completed', completed_at = NOW()
		WHERE tournament_id = $1 AND status != 'completed'`,
		tournamentID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "close unfinished pairings failed", "tournament_id", tournamentID, "error", err)
		return err
	}

	_, err = tx.Exec(
		ctx,
		`UPDATE tournament_rounds
		SET status = 'completed', completed_at = NOW()
		WHERE tournament_id = $1 AND status != 'completed'`,
		tournamentID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "close unfinished rounds failed", "tournament_id", tournamentID, "error", err)
		return err
	}

	tag, err := tx.Exec(
		ctx,
		`UPDATE tournaments
		SET status = 'completed', updated_at = NOW()
		WHERE id = $1 AND status = 'ongoing'`,
		tournamentID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "update tournament status to completed failed", "tournament_id", tournamentID, "error", err)
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotOngoing
	}

	return tx.Commit(ctx)
}

func (r *Repository) CancelTournament(ctx context.Context, tournamentID int64) ([]string, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "begin transaction failed for cancel tournament", "tournament_id", tournamentID, "error", err)
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(
		ctx,
		`UPDATE games
		SET status = 'finished', end_reason = 'aborted', ended_at = NOW()
		WHERE id IN (
			SELECT game_id FROM tournament_pairings
			WHERE tournament_id = $1 AND game_id IS NOT NULL AND status != 'completed'
		) AND status = 'active'
		RETURNING id`,
		tournamentID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "abort unfinished tournament games failed", "tournament_id", tournamentID, "error", err)
		return nil, err
	}
	var abortedGameIDs []string
	for rows.Next() {
		var gid string
		if err := rows.Scan(&gid); err == nil {
			abortedGameIDs = append(abortedGameIDs, gid)
		}
	}
	rows.Close()

	_, err = tx.Exec(
		ctx,
		`UPDATE tournament_pairings
		SET status = 'completed', completed_at = NOW()
		WHERE tournament_id = $1 AND status != 'completed'`,
		tournamentID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "close unfinished pairings failed", "tournament_id", tournamentID, "error", err)
		return nil, err
	}

	_, err = tx.Exec(
		ctx,
		`UPDATE tournament_rounds
		SET status = 'completed', completed_at = NOW()
		WHERE tournament_id = $1 AND status != 'completed'`,
		tournamentID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "close unfinished rounds failed", "tournament_id", tournamentID, "error", err)
		return nil, err
	}

	tag, err := tx.Exec(
		ctx,
		`UPDATE tournaments
		SET status = 'cancelled', updated_at = NOW()
		WHERE id = $1 AND status = 'ongoing'`,
		tournamentID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "update tournament status to cancelled failed", "tournament_id", tournamentID, "error", err)
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotOngoing
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return abortedGameIDs, nil
}

func (r *Repository) GetSwissPlayers(ctx context.Context, tournamentID int64) ([]*SwissPlayer, error) {
	var timeControlStr string
	err := r.DB.QueryRow(ctx, `SELECT time_control FROM tournaments WHERE id = $1`, tournamentID).Scan(&timeControlStr)
	if err != nil {
		slog.ErrorContext(ctx, "get tournament time control failed", "tournament_id", tournamentID, "error", err)
		return nil, err
	}
	tcMinutes, tcIncrement, _ := parseTimeControl(timeControlStr)
	mode, _, _, _ := game.ResolveTimeControlMode(tcMinutes, tcIncrement)

	rows, err := r.DB.Query(
		ctx,
		`SELECT tp.user_id, COALESCE(pr.rating, u.start_rating, 1200) AS rating, tp.score, tp.wins, tp.draws, tp.losses, tp.games_played
		FROM tournament_players tp
		LEFT JOIN users u ON tp.user_id = u.id
		LEFT JOIN player_ratings pr ON pr.user_id = tp.user_id AND pr.mode = $2
		WHERE tp.tournament_id = $1`,
		tournamentID,
		mode,
	)
	if err != nil {
		slog.ErrorContext(ctx, "get swiss players query failed", "tournament_id", tournamentID, "error", err)
		return nil, err
	}
	defer rows.Close()

	playerMap := make(map[int64]*SwissPlayer)
	for rows.Next() {
		var p SwissPlayer
		p.PreviousOpponents = make(map[int64]bool)
		if err := rows.Scan(&p.UserID, &p.Rating, &p.Score, &p.Wins, &p.Draws, &p.Losses, &p.GamesPlayed); err != nil {
			return nil, err
		}
		playerMap[p.UserID] = &p
	}
	rows.Close()

	pairingRows, err := r.DB.Query(
		ctx,
		`SELECT white_player_id, black_player_id, is_bye, status
		FROM tournament_pairings
		WHERE tournament_id = $1
		ORDER BY created_at ASC`,
		tournamentID,
	)
	if err == nil {
		defer pairingRows.Close()
		for pairingRows.Next() {
			var whiteID, blackID *int64
			var isBye bool
			var status string
			if err := pairingRows.Scan(&whiteID, &blackID, &isBye, &status); err == nil {
				if isBye && whiteID != nil {
					if sp, ok := playerMap[*whiteID]; ok {
						sp.ReceivedBye = true
					}
				} else if whiteID != nil && blackID != nil {
					wSp, wOk := playerMap[*whiteID]
					bSp, bOk := playerMap[*blackID]
					if wOk {
						wSp.PreviousOpponents[*blackID] = true
						wSp.WhiteCount++
						if wSp.LastColor == "W" {
							wSp.ColorStreak++
						} else {
							wSp.LastColor = "W"
							wSp.ColorStreak = 1
						}
					}
					if bOk {
						bSp.PreviousOpponents[*whiteID] = true
						bSp.BlackCount++
						if bSp.LastColor == "B" {
							bSp.ColorStreak++
						} else {
							bSp.LastColor = "B"
							bSp.ColorStreak = 1
						}
					}
				}
			}
		}
	}

	for _, sp := range playerMap {
		for oppID := range sp.PreviousOpponents {
			if opp, ok := playerMap[oppID]; ok {
				sp.Buchholz += opp.Score
			}
		}
	}

	players := make([]*SwissPlayer, 0, len(playerMap))
	for _, p := range playerMap {
		players = append(players, p)
	}

	sort.Slice(players, func(i, j int) bool {
		return players[i].UserID < players[j].UserID
	})

	return players, nil
}

func (r *Repository) SaveRoundPairings(ctx context.Context, tournamentID int64, roundNumber int, pairings []SwissPairingResult, gameRepo ...game.GameRepository) ([]*TournamentPairing, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var timeControlStr string
	_ = tx.QueryRow(ctx, `SELECT time_control FROM tournaments WHERE id = $1`, tournamentID).Scan(&timeControlStr)
	tcMinutes, tcIncrement, _ := parseTimeControl(timeControlStr)

	var createdGameIDs []string
	saved := false
	defer func() {
		if saved {
			return
		}
		cleanupCtx := context.WithoutCancel(ctx)
		for _, gameID := range createdGameIDs {
			_ = gameRepo[0].DeleteGameWithMoves(cleanupCtx, gameID)
		}
	}()

	var roundID int64
	err = tx.QueryRow(
		ctx,
		`INSERT INTO tournament_rounds (tournament_id, round_number, status, started_at)
		VALUES ($1, $2, 'ongoing', NOW())
		ON CONFLICT (tournament_id, round_number)
		DO UPDATE SET status = 'ongoing', started_at = NOW()
		RETURNING id`,
		tournamentID,
		roundNumber,
	).Scan(&roundID)
	if err != nil {
		slog.ErrorContext(ctx, "insert or update round failed", "tournament_id", tournamentID, "round", roundNumber, "error", err)
		return nil, err
	}

	resultPairings := make([]*TournamentPairing, 0, len(pairings))
	for _, p := range pairings {
		var tp TournamentPairing
		tp.TournamentID = tournamentID
		tp.RoundID = roundID
		tp.WhitePlayerID = p.WhitePlayerID
		tp.BlackPlayerID = p.BlackPlayerID
		tp.IsBye = p.IsBye
		tp.Status = "pending"

		if p.IsBye {
			resultStr := ResultBye
			tp.Result = &resultStr
			tp.Status = "completed"

			err = tx.QueryRow(
				ctx,
				`INSERT INTO tournament_pairings (
					tournament_id, round_id, white_player_id, black_player_id, result, status, is_bye, created_at, completed_at
				) VALUES ($1, $2, $3, NULL, $4, $5, TRUE, NOW(), NOW())
				RETURNING id, created_at, completed_at`,
				tournamentID, roundID, p.WhitePlayerID, resultStr, tp.Status,
			).Scan(&tp.ID, &tp.CreatedAt, &tp.CompletedAt)

			if err != nil {
				return nil, err
			}

			if p.WhitePlayerID != nil {
				_, err = tx.Exec(
					ctx,
					`UPDATE tournament_players
					SET score = score + 1.0, wins = wins + 1, games_played = games_played + 1
					WHERE tournament_id = $1 AND user_id = $2`,
					tournamentID, *p.WhitePlayerID,
				)
				if err != nil {
					return nil, err
				}
			}
		} else {
			var createdGameID *string
			if len(gameRepo) > 0 && gameRepo[0] != nil && p.WhitePlayerID != nil && p.BlackPlayerID != nil {
				gID, gErr := gameRepo[0].CreateGame(ctx, *p.WhitePlayerID, *p.BlackPlayerID, tcMinutes, tcIncrement, false)
				if gErr != nil {
					slog.ErrorContext(ctx, "create tournament game failed", "tournament_id", tournamentID, "round", roundNumber, "error", gErr)
					return nil, gErr
				}
				createdGameIDs = append(createdGameIDs, gID)
				createdGameID = &gID
				tp.GameID = &gID
			}

			err = tx.QueryRow(
				ctx,
				`INSERT INTO tournament_pairings (
					tournament_id, round_id, white_player_id, black_player_id, game_id, status, is_bye, created_at
				) VALUES ($1, $2, $3, $4, $5, 'pending', FALSE, NOW())
				RETURNING id, created_at`,
				tournamentID, roundID, p.WhitePlayerID, p.BlackPlayerID, createdGameID,
			).Scan(&tp.ID, &tp.CreatedAt)

			if err != nil {
				return nil, err
			}
		}
		resultPairings = append(resultPairings, &tp)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	saved = true

	return resultPairings, nil
}

func (r *Repository) UpdatePairingOnGameCompleted(ctx context.Context, gameID string, result string) (int64, bool, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var pairingID int64
	var tournamentID int64
	var roundID int64
	var whiteID, blackID *int64

	normResult := result
	switch result {
	case "1-0":
		normResult = ResultWhiteWin
	case "0-1":
		normResult = ResultBlackWin
	case "1/2-1/2":
		normResult = ResultDraw
	}

	err = tx.QueryRow(
		ctx,
		`UPDATE tournament_pairings
		SET result = $2, status = 'completed', completed_at = NOW()
		WHERE game_id = $1 AND status != 'completed'
		RETURNING id, tournament_id, round_id, white_player_id, black_player_id`,
		gameID,
		normResult,
	).Scan(&pairingID, &tournamentID, &roundID, &whiteID, &blackID)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		slog.ErrorContext(ctx, "update tournament pairing on game completion failed", "game_id", gameID, "error", err)
		return 0, false, err
	}

	switch normResult {
	case ResultWhiteWin:
		err = addPlayerResult(ctx, tx, tournamentID, whiteID, 1.0, 1, 0, 0)
		if err == nil {
			err = addPlayerResult(ctx, tx, tournamentID, blackID, 0, 0, 0, 1)
		}
	case ResultBlackWin:
		err = addPlayerResult(ctx, tx, tournamentID, blackID, 1.0, 1, 0, 0)
		if err == nil {
			err = addPlayerResult(ctx, tx, tournamentID, whiteID, 0, 0, 0, 1)
		}
	case ResultDraw:
		err = addPlayerResult(ctx, tx, tournamentID, whiteID, 0.5, 0, 1, 0)
		if err == nil {
			err = addPlayerResult(ctx, tx, tournamentID, blackID, 0.5, 0, 1, 0)
		}
	}
	if err != nil {
		slog.ErrorContext(ctx, "update tournament player score failed", "game_id", gameID, "error", err)
		return 0, false, err
	}

	var lockedRoundID int64
	err = tx.QueryRow(ctx, `SELECT id FROM tournament_rounds WHERE id = $1 FOR UPDATE`, roundID).Scan(&lockedRoundID)
	if err != nil {
		return 0, false, err
	}

	var uncompletedCount int
	err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM tournament_pairings WHERE round_id = $1 AND status != 'completed'`, roundID).Scan(&uncompletedCount)
	if err != nil {
		return 0, false, err
	}

	roundCompleted := uncompletedCount == 0
	if roundCompleted {
		_, err = tx.Exec(ctx, `UPDATE tournament_rounds SET status = 'completed', completed_at = NOW() WHERE id = $1`, roundID)
		if err != nil {
			return 0, false, err
		}

		var currentRound, totalRounds int
		err = tx.QueryRow(ctx, `SELECT current_round, total_rounds FROM tournaments WHERE id = $1`, tournamentID).Scan(&currentRound, &totalRounds)
		if err != nil {
			return 0, false, err
		}

		if currentRound >= totalRounds {
			_, err = tx.Exec(ctx, `UPDATE tournaments SET status = 'completed', updated_at = NOW() WHERE id = $1`, tournamentID)
		} else {
			_, err = tx.Exec(ctx, `UPDATE tournaments SET current_round = current_round + 1, updated_at = NOW() WHERE id = $1`, tournamentID)
		}
		if err != nil {
			return 0, false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}

	return tournamentID, roundCompleted, nil
}

func addPlayerResult(ctx context.Context, tx pgx.Tx, tournamentID int64, userID *int64, score float64, wins int, draws int, losses int) error {
	if userID == nil {
		return nil
	}

	_, err := tx.Exec(
		ctx,
		`UPDATE tournament_players
		SET score = score + $3,
			wins = wins + $4,
			draws = draws + $5,
			losses = losses + $6,
			games_played = games_played + 1
		WHERE tournament_id = $1 AND user_id = $2`,
		tournamentID,
		*userID,
		score,
		wins,
		draws,
		losses,
	)
	return err
}

func (r *Repository) GetStandings(ctx context.Context, tournamentID int64) ([]*StandingsPlayerResponse, error) {
	rows, err := r.DB.Query(
		ctx,
		`SELECT tp.user_id, u.username, tp.score, tp.wins, tp.draws, tp.losses, tp.games_played
		FROM tournament_players tp
		JOIN users u ON u.id = tp.user_id
		WHERE tp.tournament_id = $1`,
		tournamentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	playerMap := make(map[int64]*StandingsPlayerResponse)
	var standings []*StandingsPlayerResponse

	for rows.Next() {
		var p StandingsPlayerResponse
		if err := rows.Scan(&p.PlayerID, &p.Username, &p.Score, &p.Wins, &p.Draws, &p.Losses, &p.GamesPlayed); err != nil {
			return nil, err
		}
		standings = append(standings, &p)
		playerMap[p.PlayerID] = &p
	}

	pRows, err := r.DB.Query(
		ctx,
		`SELECT white_player_id, black_player_id
		FROM tournament_pairings
		WHERE tournament_id = $1 AND status = 'completed' AND is_bye = FALSE AND result IS NOT NULL`,
		tournamentID,
	)
	if err == nil {
		defer pRows.Close()
		for pRows.Next() {
			var wID, bID *int64
			if err := pRows.Scan(&wID, &bID); err == nil && wID != nil && bID != nil {
				if wPlayer, ok := playerMap[*wID]; ok {
					if bPlayer, ok2 := playerMap[*bID]; ok2 {
						wPlayer.Buchholz += bPlayer.Score
						bPlayer.Buchholz += wPlayer.Score
					}
				}
			}
		}
	}

	sort.Slice(standings, func(i, j int) bool {
		if standings[i].Score != standings[j].Score {
			return standings[i].Score > standings[j].Score
		}
		if standings[i].Buchholz != standings[j].Buchholz {
			return standings[i].Buchholz > standings[j].Buchholz
		}
		if standings[i].Wins != standings[j].Wins {
			return standings[i].Wins > standings[j].Wins
		}
		return standings[i].PlayerID < standings[j].PlayerID
	})

	for i := range standings {
		standings[i].Rank = i + 1
	}

	return standings, nil
}

func (r *Repository) GetRounds(ctx context.Context, tournamentID int64, roundNumber ...int) ([]*RoundResponse, error) {
	queryRounds := `SELECT id, round_number, status FROM tournament_rounds WHERE tournament_id = $1`
	argsRounds := []interface{}{tournamentID}
	if len(roundNumber) > 0 && roundNumber[0] > 0 {
		queryRounds += ` AND round_number = $2`
		argsRounds = append(argsRounds, roundNumber[0])
	}
	queryRounds += ` ORDER BY round_number ASC`

	rRows, err := r.DB.Query(ctx, queryRounds, argsRounds...)
	if err != nil {
		return nil, err
	}
	defer rRows.Close()

	roundMap := make(map[int64]*RoundResponse)
	var roundList []*RoundResponse

	for rRows.Next() {
		var roundID int64
		var res RoundResponse
		if err := rRows.Scan(&roundID, &res.RoundNumber, &res.Status); err != nil {
			return nil, err
		}
		res.ID = strconv.FormatInt(roundID, 10)
		res.Pairings = make([]*PairingResponse, 0)
		roundList = append(roundList, &res)
		roundMap[roundID] = &res
	}

	queryPairings := `SELECT 
		tp.id, tp.round_id, tr.round_number, 
		tp.white_player_id, COALESCE(uw.username, ''), 
		tp.black_player_id, COALESCE(ub.username, ''), 
		tp.game_id, COALESCE(tp.result, ''), tp.status, tp.is_bye
	FROM tournament_pairings tp
	JOIN tournament_rounds tr ON tr.id = tp.round_id
	LEFT JOIN users uw ON uw.id = tp.white_player_id
	LEFT JOIN users ub ON ub.id = tp.black_player_id
	WHERE tp.tournament_id = $1`
	argsPairings := []interface{}{tournamentID}
	if len(roundNumber) > 0 && roundNumber[0] > 0 {
		queryPairings += ` AND tr.round_number = $2`
		argsPairings = append(argsPairings, roundNumber[0])
	}
	queryPairings += ` ORDER BY tr.round_number ASC, tp.id ASC`

	pRows, err := r.DB.Query(ctx, queryPairings, argsPairings...)
	if err != nil {
		return nil, err
	}
	defer pRows.Close()

	for pRows.Next() {
		var pID, rID int64
		var p PairingResponse
		var wName, bName string
		if err := pRows.Scan(
			&pID, &rID, &p.RoundNumber,
			&p.WhitePlayerID, &wName,
			&p.BlackPlayerID, &bName,
			&p.GameID, &p.Result, &p.Status, &p.IsBye,
		); err != nil {
			return nil, err
		}
		p.ID = strconv.FormatInt(pID, 10)
		p.RoundID = strconv.FormatInt(rID, 10)
		if p.WhitePlayerID != nil {
			p.WhiteUsername = wName
		}
		if p.BlackPlayerID != nil {
			p.BlackUsername = bName
		}

		if round, ok := roundMap[rID]; ok {
			round.Pairings = append(round.Pairings, &p)
		}
	}

	return roundList, nil
}

func (r *Repository) GetGames(ctx context.Context, tournamentID int64, userID ...int64) ([]*TournamentGameItem, error) {
	query := `SELECT 
		tp.game_id, tp.id, tr.round_number,
		COALESCE(tp.white_player_id, 0), COALESCE(uw.username, ''),
		COALESCE(tp.black_player_id, 0), COALESCE(ub.username, ''),
		COALESCE(tp.result, ''), tp.status
	FROM tournament_pairings tp
	JOIN tournament_rounds tr ON tr.id = tp.round_id
	LEFT JOIN users uw ON uw.id = tp.white_player_id
	LEFT JOIN users ub ON ub.id = tp.black_player_id
	WHERE tp.tournament_id = $1 AND tp.game_id IS NOT NULL`
	args := []interface{}{tournamentID}

	if len(userID) > 0 && userID[0] > 0 {
		query += ` AND (tp.white_player_id = $2 OR tp.black_player_id = $2)`
		args = append(args, userID[0])
	}
	query += ` ORDER BY tr.round_number ASC, tp.id ASC`

	rows, err := r.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []*TournamentGameItem
	for rows.Next() {
		var item TournamentGameItem
		var pairingID int64
		var gameID *string
		if err := rows.Scan(
			&gameID, &pairingID, &item.RoundNumber,
			&item.WhitePlayerID, &item.WhiteUsername,
			&item.BlackPlayerID, &item.BlackUsername,
			&item.Result, &item.Status,
		); err != nil {
			return nil, err
		}
		if gameID != nil {
			item.GameID = *gameID
		}
		item.PairingID = strconv.FormatInt(pairingID, 10)
		games = append(games, &item)
	}

	return games, nil
}

func (r *Repository) UpdateTotalRounds(ctx context.Context, tournamentID int64, totalRounds int) error {
	_, err := r.DB.Exec(ctx, `UPDATE tournaments SET total_rounds = $2, updated_at = NOW() WHERE id = $1`, tournamentID, totalRounds)
	return err
}

func (r *Repository) GetRoundWinners(ctx context.Context, tournamentID int64, roundID int64) ([]int64, error) {
	var timeControlStr string
	err := r.DB.QueryRow(ctx, `SELECT time_control FROM tournaments WHERE id = $1`, tournamentID).Scan(&timeControlStr)
	if err != nil {
		slog.ErrorContext(ctx, "get tournament time control failed", "tournament_id", tournamentID, "error", err)
		return nil, err
	}
	tcMinutes, tcIncrement, _ := parseTimeControl(timeControlStr)
	mode, _, _, _ := game.ResolveTimeControlMode(tcMinutes, tcIncrement)

	rows, err := r.DB.Query(
		ctx,
		`SELECT tp.white_player_id, tp.black_player_id, tp.result, tp.is_bye,
			COALESCE(wr.rating, wu.start_rating, 1200), COALESCE(br.rating, bu.start_rating, 1200)
		FROM tournament_pairings tp
		LEFT JOIN users wu ON wu.id = tp.white_player_id
		LEFT JOIN player_ratings wr ON wr.user_id = tp.white_player_id AND wr.mode = $3
		LEFT JOIN users bu ON bu.id = tp.black_player_id
		LEFT JOIN player_ratings br ON br.user_id = tp.black_player_id AND br.mode = $3
		WHERE tp.tournament_id = $1 AND tp.round_id = $2 AND tp.status = 'completed'
		ORDER BY tp.id ASC`,
		tournamentID, roundID, mode,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var winners []int64
	for rows.Next() {
		var wID, bID *int64
		var result *string
		var isBye bool
		var whiteRating, blackRating int
		if err := rows.Scan(&wID, &bID, &result, &isBye, &whiteRating, &blackRating); err != nil {
			return nil, err
		}

		if isBye && wID != nil {
			winners = append(winners, *wID)
		} else if result != nil {
			switch *result {
			case ResultWhiteWin:
				if wID != nil {
					winners = append(winners, *wID)
				}
			case ResultBlackWin:
				if bID != nil {
					winners = append(winners, *bID)
				}
			case ResultDraw:
				if bID != nil && blackRating > whiteRating {
					winners = append(winners, *bID)
				} else if wID != nil {
					winners = append(winners, *wID)
				}
			}
		}
	}

	return winners, nil
}

func (r *Repository) DeleteOldTournaments(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tag, err := r.DB.Exec(
		ctx,
		`DELETE FROM tournaments
		WHERE status IN ('completed', 'cancelled')
		  AND updated_at < $1`,
		cutoff,
	)
	if err != nil {
		slog.ErrorContext(ctx, "delete old tournaments repository failed", "error", err)
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) UndoStartTournament(ctx context.Context, tournamentID int64) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "begin transaction failed for undo start tournament", "tournament_id", tournamentID, "error", err)
		return err
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	_, err = tx.Exec(ctx, `DELETE FROM tournament_rounds WHERE tournament_id = $1`, tournamentID)
	if err != nil {
		slog.ErrorContext(ctx, "delete rounds failed for undo start tournament", "tournament_id", tournamentID, "error", err)
		return err
	}

	_, err = tx.Exec(ctx,
		`UPDATE tournaments
		SET status = 'registration', current_round = 0, updated_at = NOW()
		WHERE id = $1 AND status = 'ongoing'`,
		tournamentID,
	)

	if err != nil {
		slog.ErrorContext(ctx, "reset tournament status failed for undo start tournament", "tournament_id", tournamentID, "error", err)
		return err
	}

	return tx.Commit(ctx)
}

func (r *Repository) RemoveUserFromTournaments(ctx context.Context, userID int64) ([]int64, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	rows, err := tx.Query(
		ctx,
		`UPDATE tournaments
		SET status = 'cancelled', updated_at = NOW()
		WHERE created_by = $1 AND status IN ('registration', 'upcoming')
		RETURNING id`,
		userID,
	)

	if err != nil {
		slog.ErrorContext(ctx, "cancel user tournaments failed", "user_id", userID, "error", err)
		return nil, err
	}

	cancelledIDs := []int64{}
	for rows.Next() {
		var id int64
		err := rows.Scan(&id)
		if err != nil {
			rows.Close()
			return nil, err
		}
		cancelledIDs = append(cancelledIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	_, err = tx.Exec(
		ctx,
		`DELETE FROM tournament_players tp
		USING tournaments t
		WHERE t.id = tp.tournament_id AND tp.user_id = $1 AND t.status IN ('registration', 'upcoming')`,
		userID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "remove user from not started tournaments failed", "user_id", userID, "error", err)
		return nil, err
	}

	_, err = tx.Exec(
		ctx,
		`UPDATE tournament_players tp
		SET withdrawn = TRUE
		FROM tournaments t
		WHERE t.id = tp.tournament_id AND tp.user_id = $1 AND t.status = 'ongoing'`,
		userID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "mark user withdrawn failed", "user_id", userID, "error", err)
		return nil, err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, err
	}

	return cancelledIDs, nil
}

func (r *Repository) GetWithdrawnPlayers(ctx context.Context, tournamentID int64) (map[int64]bool, error) {
	rows, err := r.DB.Query(
		ctx,
		`SELECT user_id FROM tournament_players WHERE tournament_id = $1 AND withdrawn = TRUE`,
		tournamentID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "get withdrawn players failed", "tournament_id", tournamentID, "error", err)
		return nil, err
	}
	defer rows.Close()

	withdrawn := make(map[int64]bool)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		withdrawn[userID] = true
	}

	return withdrawn, rows.Err()
}

func (r *Repository) FinishRoundIfNoGames(ctx context.Context, tournamentID int64, roundNumber int) (bool, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var roundID int64
	var roundStatus string
	err = tx.QueryRow(
		ctx,
		`SELECT id, status FROM tournament_rounds WHERE tournament_id = $1 AND round_number = $2 FOR UPDATE`,
		tournamentID,
		roundNumber,
	).Scan(&roundID, &roundStatus)
	if err != nil {
		slog.ErrorContext(ctx, "find round failed", "tournament_id", tournamentID, "round", roundNumber, "error", err)
		return false, err
	}
	if roundStatus == RoundStatusCompleted {
		return false, nil
	}

	var pendingCount int
	err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM tournament_pairings WHERE round_id = $1 AND status != 'completed'`, roundID).Scan(&pendingCount)
	if err != nil {
		return false, err
	}
	if pendingCount > 0 {
		return false, nil
	}

	_, err = tx.Exec(ctx, `UPDATE tournament_rounds SET status = 'completed', completed_at = NOW() WHERE id = $1`, roundID)
	if err != nil {
		return false, err
	}

	var currentRound, totalRounds int
	err = tx.QueryRow(ctx, `SELECT current_round, total_rounds FROM tournaments WHERE id = $1 FOR UPDATE`, tournamentID).Scan(&currentRound, &totalRounds)
	if err != nil {
		return false, err
	}

	if currentRound >= totalRounds {
		_, err = tx.Exec(ctx, `UPDATE tournaments SET status = 'completed', updated_at = NOW() WHERE id = $1`, tournamentID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE tournaments SET current_round = current_round + 1, updated_at = NOW() WHERE id = $1`, tournamentID)
	}
	if err != nil {
		slog.ErrorContext(ctx, "move tournament to next round failed", "tournament_id", tournamentID, "error", err)
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}

	return true, nil
}

func (r *Repository) UpdateInviteCode(ctx context.Context, tournamentID int64, newCode string, expiresAt time.Time) error {
	_, err := r.DB.Exec(
		ctx,
		`UPDATE tournaments SET invite_code = $1, invite_code_expires_at = $2, updated_at = NOW() WHERE id = $3`,
		newCode,
		expiresAt,
		tournamentID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "update invite code failed", "tournament_id", tournamentID, "error", err)
		return err
	}
	return nil
}
