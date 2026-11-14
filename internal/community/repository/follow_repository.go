package repository

import (
	"context"
	"log/slog"

	"github.com/here-arjun-1/Caisaara-backend/internal/community/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FollowRepository struct {
	DB *pgxpool.Pool
}

func NewFollowRepository(db *pgxpool.Pool) *FollowRepository {
	return &FollowRepository{
		DB: db,
	}
}

func (r *FollowRepository) Follow(ctx context.Context, followerID, followingID int64) error {
	_, err := r.DB.Exec(ctx,
		`INSERT INTO user_follows (follower_id, following_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`,
		followerID, followingID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "follow user failed", "follower_id", followerID, "following_id", followingID, "error", err)
		return err
	}
	return nil
}

func (r *FollowRepository) Unfollow(ctx context.Context, followerID, followingID int64) error {
	_, err := r.DB.Exec(ctx,
		`DELETE FROM user_follows
		WHERE follower_id = $1 AND following_id = $2`,
		followerID, followingID,
	)
	if err != nil {
		slog.ErrorContext(ctx, "unfollow user failed", "follower_id", followerID, "following_id", followingID, "error", err)
		return err
	}
	return nil
}

func (r *FollowRepository) IsFollowing(ctx context.Context, followerID, followingID int64) (bool, error) {
	var isFollowing bool
	err := r.DB.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM user_follows
			WHERE follower_id = $1 AND following_id = $2
		)`,
		followerID, followingID,
	).Scan(&isFollowing)
	if err != nil {
		slog.ErrorContext(ctx, "check is following failed", "follower_id", followerID, "following_id", followingID, "error", err)
		return false, err
	}
	return isFollowing, nil
}

func (r *FollowRepository) GetCounts(ctx context.Context, userID int64) (int, int, error) {
	var followers, following int
	err := r.DB.QueryRow(ctx,
		`SELECT
			(SELECT COUNT(*) FROM user_follows WHERE following_id = $1) AS followers,
			(SELECT COUNT(*) FROM user_follows WHERE follower_id = $1) AS following`,
		userID,
	).Scan(&followers, &following)
	if err != nil {
		slog.ErrorContext(ctx, "get follow counts failed", "user_id", userID, "error", err)
		return 0, 0, err
	}
	return followers, following, nil
}

func (r *FollowRepository) GetFollowers(ctx context.Context, userID int64, limit int, beforeID int64) ([]model.UserSummary, error) {
	rows, err := r.DB.Query(ctx,
		`SELECT
			u.id,
			u.username,
			pr.display_name,
			pr.avatar_url,
			EXISTS (SELECT 1 FROM user_follows f2 WHERE f2.follower_id = $1 AND f2.following_id = u.id) AS is_following
		FROM user_follows f
		JOIN users u ON u.id = f.follower_id
		LEFT JOIN player_profiles pr ON pr.user_id = u.id
		WHERE f.following_id = $1 AND ($2 = 0 OR u.id < $2)
		ORDER BY u.id DESC
		LIMIT $3`,
		userID, beforeID, limit,
	)
	if err != nil {
		slog.ErrorContext(ctx, "query followers failed", "user_id", userID, "error", err)
		return nil, err
	}
	defer rows.Close()

	users := []model.UserSummary{}
	for rows.Next() {
		var u model.UserSummary
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.IsFollowing); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *FollowRepository) GetFollowing(ctx context.Context, userID int64, limit int, beforeID int64) ([]model.UserSummary, error) {
	rows, err := r.DB.Query(ctx,
		`SELECT
			u.id,
			u.username,
			pr.display_name,
			pr.avatar_url,
			true AS is_following
		FROM user_follows f
		JOIN users u ON u.id = f.following_id
		LEFT JOIN player_profiles pr ON pr.user_id = u.id
		WHERE f.follower_id = $1 AND ($2 = 0 OR u.id < $2)
		ORDER BY u.id DESC
		LIMIT $3`,
		userID, beforeID, limit,
	)
	if err != nil {
		slog.ErrorContext(ctx, "query following failed", "user_id", userID, "error", err)
		return nil, err
	}
	defer rows.Close()

	users := []model.UserSummary{}
	for rows.Next() {
		var u model.UserSummary
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.IsFollowing); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *FollowRepository) UserExists(ctx context.Context, userID int64) (bool, error) {
	var exists bool
	err := r.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists)
	return exists, err
}

func (r *FollowRepository) IsBlocked(ctx context.Context, userID, otherID int64) (bool, error) {
	var blocked bool
	err := r.DB.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM user_blocks
			WHERE (blocker_id = $1 AND blocked_id = $2) OR (blocker_id = $2 AND blocked_id = $1)
		)`,
		userID, otherID,
	).Scan(&blocked)
	return blocked, err
}

func (r *FollowRepository) FindUsernameByID(ctx context.Context, userID int64) (string, error) {
	var username string
	err := r.DB.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
	return username, err
}
