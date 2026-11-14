package repository

import (
	"context"
	"strconv"

	"github.com/here-arjun-1/Caisaara-backend/internal/community/model"
	"github.com/here-arjun-1/Caisaara-backend/internal/notification"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostRepository struct {
	DB *pgxpool.Pool
}

func NewPostRepository(db *pgxpool.Pool) *PostRepository {
	return &PostRepository{
		DB: db,
	}
}

const postColumns = `
	p.id,
	p.user_id,
	p.kind,
	p.body,
	p.fen,
	p.game_id,
	COALESCE(p.game_pgn, ''),
	COALESCE(p.game_white_name, ''),
	COALESCE(p.game_black_name, ''),
	p.game_white_rating,
	p.game_black_rating,
	COALESCE(p.game_result, ''),
	COALESCE(p.game_end_reason, ''),
	COALESCE(p.game_mode, ''),
	p.game_played_at,
	p.created_at,
	u.username,
	pr.display_name,
	pr.avatar_url,
	(SELECT COUNT(*) FROM post_comments c WHERE c.post_id = p.id),
	(SELECT r.reaction FROM post_reactions r WHERE r.post_id = p.id AND r.user_id = $1),
	EXISTS (SELECT 1 FROM post_bookmarks b WHERE b.post_id = p.id AND b.user_id = $1),
	EXISTS (SELECT 1 FROM user_follows f WHERE f.follower_id = $1 AND f.following_id = p.user_id)
FROM posts p
JOIN users u ON u.id = p.user_id
LEFT JOIN player_profiles pr ON pr.user_id = p.user_id`

func scanPost(row pgx.Row) (*model.Post, error) {
	var p model.Post
	var game model.PostGame
	var gameID *string

	err := row.Scan(
		&p.ID,
		&p.UserID,
		&p.Kind,
		&p.Body,
		&p.FEN,
		&gameID,
		&game.PGN,
		&game.WhiteName,
		&game.BlackName,
		&game.WhiteRating,
		&game.BlackRating,
		&game.Result,
		&game.EndReason,
		&game.Mode,
		&game.PlayedAt,
		&p.CreatedAt,
		&p.Author.Username,
		&p.Author.DisplayName,
		&p.Author.AvatarURL,
		&p.CommentCount,
		&p.MyReaction,
		&p.IsBookmarked,
		&p.Author.IsFollowing,
	)
	if err != nil {
		return nil, err
	}

	p.Author.ID = p.UserID

	if gameID != nil {
		game.ID = *gameID
		p.Game = &game
	}

	return &p, nil
}

func (r *PostRepository) CreatePost(ctx context.Context, post *model.Post) (int64, error) {
	var id int64

	if post.Game == nil {
		err := r.DB.QueryRow(
			ctx,
			`INSERT INTO posts (user_id, kind, body, fen)
			VALUES ($1, $2, $3, $4)
			RETURNING id`,
			post.UserID,
			post.Kind,
			post.Body,
			post.FEN,
		).Scan(&id)

		return id, err
	}

	game := post.Game
	err := r.DB.QueryRow(
		ctx,
		`INSERT INTO posts (
			user_id, kind, body, fen,
			game_id, game_pgn, game_white_name, game_black_name,
			game_white_rating, game_black_rating, game_result, game_end_reason,
			game_mode, game_played_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id`,
		post.UserID,
		post.Kind,
		post.Body,
		post.FEN,
		game.ID,
		game.PGN,
		game.WhiteName,
		game.BlackName,
		game.WhiteRating,
		game.BlackRating,
		game.Result,
		game.EndReason,
		game.Mode,
		game.PlayedAt,
	).Scan(&id)

	return id, err
}

func (r *PostRepository) FindPostByID(ctx context.Context, viewerID, postID int64) (*model.Post, error) {
	row := r.DB.QueryRow(
		ctx,
		`SELECT `+postColumns+`
		WHERE p.id = $2`,
		viewerID,
		postID,
	)

	return scanPost(row)
}

func (r *PostRepository) ListPosts(ctx context.Context, viewerID, beforeID int64, limit int) ([]*model.Post, error) {
	rows, err := r.DB.Query(
		ctx,
		`SELECT `+postColumns+`
		WHERE ($2 = 0 OR p.id < $2)
		ORDER BY p.id DESC
		LIMIT $3`,
		viewerID,
		beforeID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPosts(rows)
}

func (r *PostRepository) ListBookmarkedPosts(ctx context.Context, userID, beforeID int64, limit int) ([]*model.Post, error) {
	rows, err := r.DB.Query(
		ctx,
		`SELECT `+postColumns+`
		JOIN post_bookmarks pb ON pb.post_id = p.id AND pb.user_id = $1
		WHERE ($2 = 0 OR p.id < $2)
		ORDER BY p.id DESC
		LIMIT $3`,
		userID,
		beforeID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPosts(rows)
}

func scanPosts(rows pgx.Rows) ([]*model.Post, error) {
	posts := []*model.Post{}
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}

	return posts, rows.Err()
}

func (r *PostRepository) FindPostOwner(ctx context.Context, postID int64) (int64, error) {
	var ownerID int64

	err := r.DB.QueryRow(
		ctx,
		`SELECT user_id FROM posts WHERE id = $1`,
		postID,
	).Scan(&ownerID)

	return ownerID, err
}

func (r *PostRepository) DeletePost(ctx context.Context, postID int64) error {
	_, err := r.DB.Exec(
		ctx,
		`DELETE FROM posts WHERE id = $1`,
		postID,
	)
	return err
}

func (r *PostRepository) CountReactions(ctx context.Context, postIDs []int64) (map[int64]map[string]int, error) {
	counts := make(map[int64]map[string]int)
	if len(postIDs) == 0 {
		return counts, nil
	}

	rows, err := r.DB.Query(
		ctx,
		`SELECT post_id, reaction, COUNT(*)
		FROM post_reactions
		WHERE post_id = ANY($1)
		GROUP BY post_id, reaction`,
		postIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var postID int64
		var reaction string
		var count int

		if err := rows.Scan(&postID, &reaction, &count); err != nil {
			return nil, err
		}
		if counts[postID] == nil {
			counts[postID] = make(map[string]int)
		}
		counts[postID][reaction] = count
	}

	return counts, rows.Err()
}

func (r *PostRepository) SetReaction(ctx context.Context, postID, userID int64, reaction string) (bool, error) {
	var isNew bool
	err := r.DB.QueryRow(
		ctx,
		`WITH existing AS (
			SELECT reaction FROM post_reactions WHERE post_id = $1 AND user_id = $2
		),
		upsert AS (
			INSERT INTO post_reactions (post_id, user_id, reaction)
			VALUES ($1, $2, $3)
			ON CONFLICT (post_id, user_id) DO UPDATE SET
				reaction   = EXCLUDED.reaction,
				created_at = now()
			RETURNING reaction
		)
		SELECT NOT EXISTS(SELECT 1 FROM existing) AS is_new
		FROM upsert`,
		postID,
		userID,
		reaction,
	).Scan(&isNew)
	return isNew, err
}

func (r *PostRepository) HasRecentReactionNotification(ctx context.Context, ownerID, postID, userID int64) (bool, error) {
	var exists bool
	err := r.DB.QueryRow(
		ctx,
		`SELECT EXISTS (
			SELECT 1 FROM notifications
			WHERE user_id = $1
			  AND type = $2
			  AND data->>'post_id' = $3
			  AND data->>'user_id' = $4
			  AND created_at > now() - interval '24 hours'
		)`,
		ownerID,
		notification.TypePostReaction,
		strconv.FormatInt(postID, 10),
		strconv.FormatInt(userID, 10),
	).Scan(&exists)
	return exists, err
}

func (r *PostRepository) DeleteReaction(ctx context.Context, postID, userID int64) error {
	_, err := r.DB.Exec(
		ctx,
		`DELETE FROM post_reactions WHERE post_id = $1 AND user_id = $2`,
		postID,
		userID,
	)
	return err
}

func (r *PostRepository) FindUsernameByID(ctx context.Context, userID int64) (string, error) {
	var username string

	err := r.DB.QueryRow(
		ctx,
		`SELECT username FROM users WHERE id = $1`,
		userID,
	).Scan(&username)

	return username, err
}

func (r *PostRepository) AddBookmark(ctx context.Context, postID, userID int64) error {
	_, err := r.DB.Exec(
		ctx,
		`INSERT INTO post_bookmarks (post_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id, post_id) DO NOTHING`,
		postID,
		userID,
	)
	return err
}

func (r *PostRepository) RemoveBookmark(ctx context.Context, postID, userID int64) error {
	_, err := r.DB.Exec(
		ctx,
		`DELETE FROM post_bookmarks WHERE post_id = $1 AND user_id = $2`,
		postID,
		userID,
	)
	return err
}
