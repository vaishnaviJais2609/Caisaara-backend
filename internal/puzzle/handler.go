package puzzle

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/middleware"
)

type Handler struct {
	Service PuzzleService
}

func NewHandler(service PuzzleService) *Handler {
	return &Handler{
		Service: service,
	}
}

func (h *Handler) GetDailyPuzzle(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	res, err := h.Service.GetDailyPuzzle(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "get daily puzzle failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get daily puzzle"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) SubmitAttempt(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req SubmitAttemptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	res, err := h.Service.SubmitAttempt(ctx, userID, req)
	if err != nil {
		slog.ErrorContext(ctx, "submit puzzle attempt failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to submit attempt"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetHint(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req HintRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
	}

	res, err := h.Service.GetHint(ctx, userID, req)
	if err != nil {
		slog.ErrorContext(ctx, "get puzzle hint failed", "user_id", userID, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetSolution(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req SolutionRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
	}

	res, err := h.Service.GetSolution(ctx, userID, req)
	if err != nil {
		slog.ErrorContext(ctx, "get puzzle solution failed", "user_id", userID, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetNextPuzzle(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	theme := c.Query("theme")

	res, err := h.Service.GetNextPuzzle(ctx, userID, theme)
	if err != nil {
		slog.ErrorContext(ctx, "get next puzzle failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get puzzle"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetUserStats(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	res, err := h.Service.GetUserStats(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "get user puzzle stats failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get puzzle stats"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetUserHistory(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit <= 0 {
		limit = 20
	}

	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		offset = 0
	}

	res, err := h.Service.GetUserHistory(ctx, userID, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "get user puzzle history failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get puzzle history"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) StartRush(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req StartRushRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	res, err := h.Service.StartRush(ctx, userID, req)
	if err != nil {
		slog.ErrorContext(ctx, "start rush failed", "user_id", userID, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) SubmitRushMove(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req SubmitRushMoveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	res, err := h.Service.SubmitRushMove(ctx, userID, req)
	if err != nil {
		slog.ErrorContext(ctx, "submit rush move failed", "user_id", userID, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) EndRush(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req EndRushRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	res, err := h.Service.EndRush(ctx, userID, req)
	if err != nil {
		slog.ErrorContext(ctx, "end rush failed", "user_id", userID, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetRushLeaderboard(c *gin.Context) {
	ctx := c.Request.Context()
	mode := c.DefaultQuery("mode", "3min")
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit <= 0 {
		limit = 50
	}

	res, err := h.Service.GetRushLeaderboard(ctx, mode, limit)
	if err != nil {
		slog.ErrorContext(ctx, "get rush leaderboard failed", "mode", mode, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get leaderboard"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetUserRushStats(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	res, err := h.Service.GetUserRushStats(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "get user rush stats failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get rush stats"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetUserRushHistory(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	mode := c.Query("mode")
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit <= 0 {
		limit = 20
	}

	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		offset = 0
	}

	res, err := h.Service.GetUserRushHistory(ctx, userID, mode, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "get user rush history failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get rush history"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetThemes(c *gin.Context) {
	ctx := c.Request.Context()
	res, err := h.Service.GetThemes(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "get puzzle themes failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get puzzle themes"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetCustomPuzzle(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var themes []string
	if t := strings.TrimSpace(c.Query("theme")); t != "" {
		themes = append(themes, t)
	}
	if ts := strings.TrimSpace(c.Query("themes")); ts != "" {
		for _, part := range strings.Split(ts, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				themes = append(themes, part)
			}
		}
	}

	minRating, _ := strconv.Atoi(c.DefaultQuery("min_rating", "0"))
	maxRating, _ := strconv.Atoi(c.DefaultQuery("max_rating", "0"))
	excludeSolvedStr := c.DefaultQuery("exclude_solved", "true")
	excludeSolved := excludeSolvedStr == "true" || excludeSolvedStr == "1"
	reviewMistakesStr := c.DefaultQuery("review_mistakes", "false")
	reviewMistakes := reviewMistakesStr == "true" || reviewMistakesStr == "1"

	filter := CustomPuzzleFilter{
		UserID:         userID,
		Themes:         themes,
		MinRating:      minRating,
		MaxRating:      maxRating,
		ExcludeSolved:  excludeSolved,
		ReviewMistakes: reviewMistakes,
	}

	res, err := h.Service.GetCustomPuzzle(ctx, userID, filter)
	if err != nil {
		slog.ErrorContext(ctx, "get custom puzzle failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get custom puzzle"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) SubmitCustomAttempt(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req SubmitCustomAttemptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	res, err := h.Service.SubmitCustomAttempt(ctx, userID, req)
	if err != nil {
		slog.ErrorContext(ctx, "submit custom attempt failed", "user_id", userID, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}
