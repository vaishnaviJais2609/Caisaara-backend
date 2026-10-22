package puzzle

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const numLimiterShards = 32

type shardedRateLimiter struct {
	shards [numLimiterShards]*limiterShard
	stop   chan struct{}
}

type limiterShard struct {
	mu      sync.Mutex
	clients map[string]*clientBucket
}

type clientBucket struct {
	tokens     float64
	lastRefill time.Time
}

func newShardedRateLimiter() *shardedRateLimiter {
	l := &shardedRateLimiter{
		stop: make(chan struct{}),
	}
	for i := 0; i < numLimiterShards; i++ {
		l.shards[i] = &limiterShard{
			clients: make(map[string]*clientBucket),
		}
	}
	go l.cleanup()
	return l
}

func (l *shardedRateLimiter) getShard(key string) *limiterShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	idx := h.Sum32() % numLimiterShards
	return l.shards[idx]
}

func (l *shardedRateLimiter) allow(key string, limit float64, window time.Duration) bool {
	shard := l.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := time.Now()
	b, ok := shard.clients[key]
	if !ok {
		if len(shard.clients) > 2000 {
			for k, v := range shard.clients {
				if now.Sub(v.lastRefill) > 5*time.Minute {
					delete(shard.clients, k)
				}
			}
		}
		shard.clients[key] = &clientBucket{
			tokens:     limit - 1,
			lastRefill: now,
		}
		return true
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * (limit / window.Seconds())
	if b.tokens > limit {
		b.tokens = limit
	}
	b.lastRefill = now

	if b.tokens < 1.0 {
		return false
	}
	b.tokens -= 1.0
	return true
}

func (l *shardedRateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			now := time.Now()
			for i := 0; i < numLimiterShards; i++ {
				shard := l.shards[i]
				shard.mu.Lock()
				for k, v := range shard.clients {
					if now.Sub(v.lastRefill) > 10*time.Minute {
						delete(shard.clients, k)
					}
				}
				shard.mu.Unlock()
			}
		}
	}
}

func rateLimitMiddleware(limit float64, window time.Duration) gin.HandlerFunc {
	limiter := newShardedRateLimiter()
	return func(c *gin.Context) {
		key := c.ClientIP()
		if userID, exists := c.Get("userID"); exists {
			key = fmt.Sprintf("user:%v", userID)
		}
		if !limiter.allow(key, limit, window) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded, please slow down",
			})
			return
		}
		c.Next()
	}
}

func RegisterRoutes(handler *Handler, r *gin.Engine, authMiddleware gin.HandlerFunc) {
	puzzles := r.Group("/puzzles")
	puzzles.Use(authMiddleware)
	puzzles.Use(rateLimitMiddleware(120, time.Minute))
	{
		puzzles.GET("/daily", handler.GetDailyPuzzle)
		puzzles.POST("/daily/attempt", handler.SubmitAttempt)
		puzzles.POST("/daily/hint", handler.GetHint)
		puzzles.POST("/daily/solution", handler.GetSolution)
		puzzles.GET("/next", handler.GetNextPuzzle)
		puzzles.GET("/stats", handler.GetUserStats)
		puzzles.GET("/history", handler.GetUserHistory)

		puzzles.GET("/themes", handler.GetThemes)
		puzzles.GET("/custom", handler.GetCustomPuzzle)
		puzzles.POST("/custom/attempt", handler.SubmitCustomAttempt)

		rush := puzzles.Group("/rush")
		{
			rush.POST("/start", handler.StartRush)
			rush.POST("/move", handler.SubmitRushMove)
			rush.POST("/end", handler.EndRush)
			rush.GET("/leaderboard", handler.GetRushLeaderboard)
			rush.GET("/stats", handler.GetUserRushStats)
			rush.GET("/history", handler.GetUserRushHistory)
		}
	}
}
