package repository

import (
	"context"

	"github.com/here-arjun-1/Caisaara-backend/internal/auth/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PasswordResetRepository struct {
	DB *pgxpool.Pool
}

func NewPasswordResetRepository(
	db *pgxpool.Pool,
) *PasswordResetRepository {
	return &PasswordResetRepository{
		DB: db,
	}
}
func (r *PasswordResetRepository) Create(
	reset *model.PasswordResetToken,
) error {

	_, err := r.DB.Exec(
		context.Background(),
		`INSERT INTO password_reset_tokens
		(user_id, code_hash, expires_at)
		VALUES ($1, $2, $3)`,
		reset.UserID,
		reset.CodeHash,
		reset.ExpiresAt,
	)

	return err
}
func (r *PasswordResetRepository) FindLatestByUserID(
	userID int64,
) (*model.PasswordResetToken, error) {

	var reset model.PasswordResetToken

	err := r.DB.QueryRow(
		context.Background(),
		`SELECT
			id,
			user_id,
			code_hash,
			reset_token_hash,
			expires_at,
			verified_at,
			used_at,
			attempts,
			created_at
		FROM password_reset_tokens
		WHERE user_id = $1
		AND used_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1`,
		userID,
	).Scan(
		&reset.ID,
		&reset.UserID,
		&reset.CodeHash,
		&reset.ResetTokenHash,
		&reset.ExpiresAt,
		&reset.VerifiedAt,
		&reset.UsedAt,
		&reset.Attempts,
		&reset.CreatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &reset, nil
}
func (r *PasswordResetRepository) MarkVerified(
	id int64,
	resetTokenHash string,
) error {

	_, err := r.DB.Exec(
		context.Background(),
		`UPDATE password_reset_tokens
		SET
			verified_at = CURRENT_TIMESTAMP,
			reset_token_hash = $1
		WHERE id = $2
		AND used_at IS NULL`,
		resetTokenHash,
		id,
	)

	return err
}
func (r *PasswordResetRepository) FindByResetTokenHash(
	hash string,
) (*model.PasswordResetToken, error) {

	var reset model.PasswordResetToken

	err := r.DB.QueryRow(
		context.Background(),
		`SELECT
			id,
			user_id,
			code_hash,
			reset_token_hash,
			expires_at,
			verified_at,
			used_at,
			attempts,
			created_at
		FROM password_reset_tokens
		WHERE reset_token_hash = $1`,
		hash,
	).Scan(
		&reset.ID,
		&reset.UserID,
		&reset.CodeHash,
		&reset.ResetTokenHash,
		&reset.ExpiresAt,
		&reset.VerifiedAt,
		&reset.UsedAt,
		&reset.Attempts,
		&reset.CreatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &reset, nil
}
func (r *PasswordResetRepository) MarkUsed(
	id int64,
) error {

	_, err := r.DB.Exec(
		context.Background(),
		`UPDATE password_reset_tokens
		SET used_at = CURRENT_TIMESTAMP
		WHERE id = $1
		AND used_at IS NULL`,
		id,
	)

	return err
}
