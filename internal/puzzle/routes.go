package puzzle

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ipRateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientBucket
}

type clientBucket struct {
	tokens     float64
	lastRefill time.Time
}

func (l *ipRateLimiter) allow(key string, limit float64, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.clients[key]
	if !ok {
		l.clients[key] = &clientBucket{
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

func rateLimitMiddleware(limit float64, window time.Duration) gin.HandlerFunc {
	limiter := &ipRateLimiter{
		clients: make(map[string]*clientBucket),
	}
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
