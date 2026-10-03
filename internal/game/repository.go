package game

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	DB *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{
		DB: db,
	}
}

func (r *Repository) CreateGame(
	ctx context.Context,
	whitePlayerID int64,
	blackPlayerID int64,
	timeControlMinutes int,
) (string, error) {

	gameID := uuid.New()

	_, err := r.DB.Exec(
		ctx,
		`
		INSERT INTO games (
			id,
			white_player_id,
			black_player_id,
			time_control_minutes,
			status,
			started_at
		)
		VALUES ($1, $2, $3, $4, 'active', NOW())
		`,
		gameID,
		whitePlayerID,
		blackPlayerID,
		timeControlMinutes,
	)

	if err != nil {
		return "", err
	}

	return gameID.String(), nil
}
