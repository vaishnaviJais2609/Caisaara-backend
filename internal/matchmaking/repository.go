package matchmaking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type QueueRepository interface {
	AddToQueue(ctx context.Context, entry *QueueEntry, ttl time.Duration) error
	RemoveFromQueue(ctx context.Context, userID int64) (*QueueEntry, error)
	GetEntry(ctx context.Context, userID int64) (*QueueEntry, error)
	GetQueue(ctx context.Context, timeControlMinutes int, incrementSeconds int, rated bool) ([]*QueueEntry, error)
	SaveMatch(ctx context.Context, userID int64, gameID string, ttl time.Duration) error
	GetMatch(ctx context.Context, userID int64) (string, error)
	AddToBattleQueue(ctx context.Context, userID int64, rating int, ttl time.Duration) error
	RemoveFromBattleQueue(ctx context.Context, userID int64) (*QueueEntry, error)
	GetBattleQueue(ctx context.Context) ([]*QueueEntry, error)
}

type RedisQueueRepository struct {
	Client *redis.Client
}

func NewRedisQueueRepository(client *redis.Client) *RedisQueueRepository {
	return &RedisQueueRepository{
		Client: client,
	}
}

func queueKey(timeControlMinutes int, incrementSeconds int, rated bool) string {
	gameType := "unrated"
	if rated {
		gameType = "rated"
	}
	return fmt.Sprintf("matchmaking:queue:%d:%d:%s", timeControlMinutes, incrementSeconds, gameType)
}

func entryKey(userID int64) string {
	return "matchmaking:entry:" + strconv.FormatInt(userID, 10)
}

func matchKey(userID int64) string {
	return "matchmaking:match:" + strconv.FormatInt(userID, 10)
}

func (r *RedisQueueRepository) AddToQueue(ctx context.Context, entry *QueueEntry, ttl time.Duration) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	ok, err := r.Client.SetNX(ctx, entryKey(entry.UserID), data, ttl).Result()
	if err != nil {
		return err
	}
	if !ok {
		return ErrAlreadyInQueue
	}

	err = r.Client.ZAdd(ctx, queueKey(entry.TimeControlMinutes, entry.IncrementSeconds, entry.Rated), redis.Z{
		Score:  float64(entry.Rating),
		Member: entry.UserID,
	}).Err()
	if err != nil {
		_ = r.Client.Del(ctx, entryKey(entry.UserID)).Err()
		return err
	}

	return nil
}

func (r *RedisQueueRepository) RemoveFromQueue(ctx context.Context, userID int64) (*QueueEntry, error) {
	data, err := r.Client.GetDel(ctx, entryKey(userID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotInQueue
	}
	if err != nil {
		return nil, err
	}

	var entry QueueEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}

	err = r.Client.ZRem(ctx, queueKey(entry.TimeControlMinutes, entry.IncrementSeconds, entry.Rated), userID).Err()
	if err != nil {
		return nil, err
	}

	return &entry, nil
}

func (r *RedisQueueRepository) GetEntry(ctx context.Context, userID int64) (*QueueEntry, error) {
	data, err := r.Client.Get(ctx, entryKey(userID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotInQueue
	}
	if err != nil {
		return nil, err
	}

	var entry QueueEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

func (r *RedisQueueRepository) GetQueue(ctx context.Context, timeControlMinutes int, incrementSeconds int, rated bool) ([]*QueueEntry, error) {
	key := queueKey(timeControlMinutes, incrementSeconds, rated)

	members, err := r.Client.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	entries := make([]*QueueEntry, 0, len(members))
	for _, member := range members {
		userID, err := strconv.ParseInt(member, 10, 64)
		if err != nil {
			continue
		}

		entry, err := r.GetEntry(ctx, userID)
		if errors.Is(err, ErrNotInQueue) {
			_ = r.Client.ZRem(ctx, key, member).Err()
			continue
		}
		if err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func (r *RedisQueueRepository) SaveMatch(ctx context.Context, userID int64, gameID string, ttl time.Duration) error {
	return r.Client.Set(ctx, matchKey(userID), gameID, ttl).Err()
}

func (r *RedisQueueRepository) GetMatch(ctx context.Context, userID int64) (string, error) {
	gameID, err := r.Client.Get(ctx, matchKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return gameID, nil
}


func battleQueueKey() string {
	return "matchmaking:queue:battle"
}

func battleEntryKey(userID int64) string {
	return "matchmaking:entry:battle:" + strconv.FormatInt(userID, 10)
}

func (r *RedisQueueRepository) AddToBattleQueue(ctx context.Context, userID int64, rating int, ttl time.Duration) error {
	entry := &QueueEntry{
		UserID:             userID,
		Rating:             rating,
		TimeControlMinutes: 3,
		IncrementSeconds:   0,
		Rated:              true,
		JoinedAt:           time.Now(),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	ok, err := r.Client.SetNX(ctx, battleEntryKey(userID), data, ttl).Result()
	if err != nil {
		return err
	}
	if !ok {
		return ErrAlreadyInQueue
	}

	err = r.Client.ZAdd(ctx, battleQueueKey(), redis.Z{
		Score:  float64(rating),
		Member: userID,
	}).Err()
	if err != nil {
		_ = r.Client.Del(ctx, battleEntryKey(userID)).Err()
		return err
	}
	return nil
}

func (r *RedisQueueRepository) RemoveFromBattleQueue(ctx context.Context, userID int64) (*QueueEntry, error) {
	data, err := r.Client.GetDel(ctx, battleEntryKey(userID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotInQueue
	}
	if err != nil {
		return nil, err
	}

	var entry QueueEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}

	_ = r.Client.ZRem(ctx, battleQueueKey(), userID).Err()
	return &entry, nil
}

func (r *RedisQueueRepository) GetBattleQueue(ctx context.Context) ([]*QueueEntry, error) {
	key := battleQueueKey()
	members, err := r.Client.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	entries := make([]*QueueEntry, 0, len(members))
	for _, member := range members {
		userID, err := strconv.ParseInt(member, 10, 64)
		if err != nil {
			continue
		}
		data, err := r.Client.Get(ctx, battleEntryKey(userID)).Bytes()
		if errors.Is(err, redis.Nil) {
			_ = r.Client.ZRem(ctx, key, member).Err()
			continue
		}
		if err != nil {
			return nil, err
		}
		var entry QueueEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		entries = append(entries, &entry)
	}
	return entries, nil
}
