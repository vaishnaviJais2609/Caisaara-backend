package puzzle

import (
	"errors"
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

func sendError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"success": false,
		"error":   code,
		"message": message,
	})
}

func sendPuzzleError(c *gin.Context, err error) {
	if err == nil {
		return
	}

	switch {
	case errors.Is(err, ErrInvalidUCIMove):
		sendError(c, http.StatusBadRequest, "invalid_uci_move", "Invalid UCI move format")
	case errors.Is(err, ErrInvalidMode):
		sendError(c, http.StatusBadRequest, "invalid_mode", "Invalid rush mode")
	case errors.Is(err, ErrInvalidRatingRange):
		sendError(c, http.StatusBadRequest, "invalid_rating_range", "Invalid rating range: min_rating cannot exceed max_rating")
	case errors.Is(err, ErrPuzzleNotFound):
		sendError(c, http.StatusNotFound, "puzzle_not_found", "Puzzle not found")
	case errors.Is(err, ErrNoMatchingPuzzle):
		sendError(c, http.StatusNotFound, "no_matching_puzzle", "No puzzle matches the requested filters")
	case errors.Is(err, ErrRushSessionNotFound):
		sendError(c, http.StatusNotFound, "rush_session_not_found", "Rush session not found")
	case errors.Is(err, ErrPuzzleAlreadySolved):
		sendError(c, http.StatusConflict, "already_solved", "Daily puzzle already solved today")
	case errors.Is(err, ErrPuzzleAlreadyFailed):
		sendError(c, http.StatusConflict, "already_failed", "Daily puzzle already failed today")
	case errors.Is(err, ErrHintAlreadyUsed):
		sendError(c, http.StatusConflict, "hint_already_used", "Hint already used for this puzzle")
	case errors.Is(err, ErrRushSessionEnded):
		sendError(c, http.StatusConflict, "rush_session_ended", "Rush session has already ended")
	case errors.Is(err, ErrPuzzleSessionMismatch):
		sendError(c, http.StatusConflict, "session_mismatch", "Puzzle ID does not match active session")
	case errors.Is(err, ErrPuzzleNotFinished):
		sendError(c, http.StatusForbidden, "puzzle_not_finished", "Puzzle details are only available after finishing the puzzle")
	default:
		sendError(c, http.StatusInternalServerError, "internal_error", err.Error())
	}
}

func getTimeZone(c *gin.Context) string {
	if tz := c.GetHeader("X-Timezone"); tz != "" {
		return tz
	}
	return c.Query("tz")
}

func (h *Handler) GetDailyPuzzle(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	tz := getTimeZone(c)
	res, err := h.Service.GetDailyPuzzle(ctx, userID, tz)
	if err != nil {
		slog.ErrorContext(ctx, "get daily puzzle failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) SubmitAttempt(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	var req SubmitAttemptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendError(c, http.StatusBadRequest, "invalid_request_body", "Invalid request body")
		return
	}

	tz := getTimeZone(c)
	res, err := h.Service.SubmitAttempt(ctx, userID, req, tz)
	if err != nil {
		slog.ErrorContext(ctx, "submit puzzle attempt failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetHint(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	var req HintRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			sendError(c, http.StatusBadRequest, "invalid_request_body", "Invalid request body")
			return
		}
	}

	tz := getTimeZone(c)
	res, err := h.Service.GetHint(ctx, userID, req, tz)
	if err != nil {
		slog.ErrorContext(ctx, "get puzzle hint failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetSolution(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	var req SolutionRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			sendError(c, http.StatusBadRequest, "invalid_request_body", "Invalid request body")
			return
		}
	}

	tz := getTimeZone(c)
	res, err := h.Service.GetSolution(ctx, userID, req, tz)
	if err != nil {
		slog.ErrorContext(ctx, "get puzzle solution failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetPuzzleByID(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	puzzleID := strings.TrimSpace(c.Param("puzzle_id"))
	if puzzleID == "" {
		sendError(c, http.StatusBadRequest, "invalid_puzzle_id", "Puzzle ID is required")
		return
	}

	res, err := h.Service.GetPuzzleByID(ctx, userID, puzzleID)
	if err != nil {
		slog.ErrorContext(ctx, "get puzzle by id failed", "user_id", userID, "puzzle_id", puzzleID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetNextPuzzle(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	theme := c.Query("theme")

	res, err := h.Service.GetNextPuzzle(ctx, userID, theme)
	if err != nil {
		slog.ErrorContext(ctx, "get next puzzle failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) SubmitNextAttempt(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	var req SubmitCustomAttemptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendError(c, http.StatusBadRequest, "invalid_request_body", "Invalid request body")
		return
	}

	req.Rated = true
	res, err := h.Service.SubmitCustomAttempt(ctx, userID, req)
	if err != nil {
		slog.ErrorContext(ctx, "submit next attempt failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetUserStats(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	res, err := h.Service.GetUserStats(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "get user puzzle stats failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetUserHistory(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
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
	if offset > 5000 {
		offset = 5000
	}

	res, err := h.Service.GetUserHistory(ctx, userID, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "get user puzzle history failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) StartRush(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	var req StartRushRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendError(c, http.StatusBadRequest, "invalid_request_body", "Invalid request body")
		return
	}

	res, err := h.Service.StartRush(ctx, userID, req)
	if err != nil {
		slog.ErrorContext(ctx, "start rush failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) SubmitRushMove(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	var req SubmitRushMoveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendError(c, http.StatusBadRequest, "invalid_request_body", "Invalid request body")
		return
	}

	res, err := h.Service.SubmitRushMove(ctx, userID, req)
	if err != nil {
		slog.ErrorContext(ctx, "submit rush move failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) EndRush(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	var req EndRushRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendError(c, http.StatusBadRequest, "invalid_request_body", "Invalid request body")
		return
	}

	res, err := h.Service.EndRush(ctx, userID, req)
	if err != nil {
		slog.ErrorContext(ctx, "end rush failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
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
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetUserRushStats(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	res, err := h.Service.GetUserRushStats(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "get user rush stats failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetUserRushHistory(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
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
	if offset > 5000 {
		offset = 5000
	}

	res, err := h.Service.GetUserRushHistory(ctx, userID, mode, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "get user rush history failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetThemes(c *gin.Context) {
	ctx := c.Request.Context()
	res, err := h.Service.GetThemes(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "get puzzle themes failed", "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetCustomPuzzle(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
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
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) SubmitCustomAttempt(c *gin.Context) {
	ctx := c.Request.Context()
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		sendError(c, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	var req SubmitCustomAttemptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		sendError(c, http.StatusBadRequest, "invalid_request_body", "Invalid request body")
		return
	}

	res, err := h.Service.SubmitCustomAttempt(ctx, userID, req)
	if err != nil {
		slog.ErrorContext(ctx, "submit custom attempt failed", "user_id", userID, "error", err)
		sendPuzzleError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}
