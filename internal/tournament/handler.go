package tournament

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/middleware"
	"github.com/here-arjun-1/Caisaara-backend/internal/config"
	"github.com/here-arjun-1/Caisaara-backend/internal/privacy"
)

type Handler struct {
	Service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{
		Service: service,
	}
}

func (h *Handler) CreateTournament(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "user not authenticated",
		})
		return
	}

	var req CreateTournamentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	ctx := c.Request.Context()
	t, err := h.Service.CreateTournament(ctx, userID, req)
	if err != nil {
		if errors.Is(err, ErrInvalidName) ||
			errors.Is(err, ErrInvalidTimeControl) ||
			errors.Is(err, ErrInvalidFormat) ||
			errors.Is(err, ErrInvalidVisibility) ||
			errors.Is(err, ErrInvalidMinPlayers) ||
			errors.Is(err, ErrInvalidMaxPlayers) ||
			errors.Is(err, ErrInvalidTotalRounds) ||
			errors.Is(err, ErrInvalidMinRating) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		slog.ErrorContext(ctx, "create tournament handler failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	c.JSON(http.StatusCreated, ToTournamentResponse(t))
}

func (h *Handler) ListPublicTournaments(c *gin.Context) {
	ctx := c.Request.Context()
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	if page, err := strconv.Atoi(c.Query("page")); err == nil && page > 1 && offset == 0 {
		if limit <= 0 {
			limit = 20
		}
		offset = (page - 1) * limit
	}

	list, err := h.Service.ListPublicTournaments(ctx, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "list public tournaments handler failed", "limit", limit, "offset", offset, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	res := make([]*TournamentDetailsResponse, 0, len(list))
	for _, tw := range list {
		res = append(res, ToTournamentDetailsResponse(tw))
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetTournamentDetails(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid tournament id",
		})
		return
	}

	ctx := c.Request.Context()
	tw, err := h.Service.GetTournamentDetails(ctx, id)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "tournament not found",
			})
			return
		}

		slog.ErrorContext(ctx, "get tournament details handler failed", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	res := ToTournamentDetailsResponse(tw)

	userID, ok := middleware.GetUserID(c)
	if ok && userID == tw.CreatedBy && tw.InviteCode != nil {
		res.InviteCode = *tw.InviteCode
		res.Link = config.AppBaseURL + "/tournaments/invite/" + *tw.InviteCode
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetTournamentByInviteCode(c *gin.Context) {
	code := c.Param("code")
	ctx := c.Request.Context()

	tw, err := h.Service.GetTournamentByInviteCode(ctx, code)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "tournament not found",
			})
			return
		}
		if errors.Is(err, ErrInviteCodeExpired) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		slog.ErrorContext(ctx, "get tournament by invite code handler failed", "code", code, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	res := ToTournamentDetailsResponse(tw)
	if tw.InviteCode != nil && *tw.InviteCode != "" {
		res.InviteCode = *tw.InviteCode
		res.Link = config.AppBaseURL + "/tournaments/invite/" + *tw.InviteCode
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) JoinTournament(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "user not authenticated",
		})
		return
	}

	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid tournament id",
		})
		return
	}

	var req JoinTournamentRequest
	_ = c.ShouldBindJSON(&req)

	inviteCode := strings.TrimSpace(req.InviteCode)
	if inviteCode == "" {
		inviteCode = strings.TrimSpace(c.Query("invite_code"))
	}

	ctx := c.Request.Context()
	_, err = h.Service.JoinTournament(ctx, id, userID, inviteCode)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "tournament not found",
			})
			return
		}

		if errors.Is(err, privacy.ErrNotAllowed) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": err.Error(),
			})
			return
		}

		if errors.Is(err, ErrNotRegistration) ||
			errors.Is(err, ErrTournamentFull) ||
			errors.Is(err, ErrAlreadyJoined) ||
			errors.Is(err, ErrInvalidInviteCode) ||
			errors.Is(err, ErrInviteCodeExpired) ||
			errors.Is(err, ErrRatingTooLow) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		slog.ErrorContext(ctx, "join tournament handler failed", "tournament_id", id, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, JoinTournamentResponse{
		Message:      "successfully joined tournament",
		TournamentID: strconv.FormatInt(id, 10),
		UserID:       userID,
		Status:       "joined",
	})
}

func (h *Handler) JoinTournamentByInviteCode(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "user not authenticated",
		})
		return
	}

	code := c.Param("code")
	ctx := c.Request.Context()

	player, err := h.Service.JoinTournamentByInviteCode(ctx, code, userID)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "tournament not found",
			})
			return
		}

		if errors.Is(err, privacy.ErrNotAllowed) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": err.Error(),
			})
			return
		}

		if errors.Is(err, ErrNotRegistration) ||
			errors.Is(err, ErrTournamentFull) ||
			errors.Is(err, ErrAlreadyJoined) ||
			errors.Is(err, ErrInvalidInviteCode) ||
			errors.Is(err, ErrInviteCodeExpired) ||
			errors.Is(err, ErrRatingTooLow) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		slog.ErrorContext(ctx, "join tournament by invite code handler failed", "code", code, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, JoinTournamentResponse{
		Message:      "successfully joined tournament",
		TournamentID: strconv.FormatInt(player.TournamentID, 10),
		UserID:       userID,
		Status:       "joined",
	})
}

func (h *Handler) LeaveTournament(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "user not authenticated",
		})
		return
	}

	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid tournament id",
		})
		return
	}

	ctx := c.Request.Context()
	err = h.Service.LeaveTournament(ctx, id, userID)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "tournament not found",
			})
			return
		}

		if errors.Is(err, ErrNotJoined) || errors.Is(err, ErrCannotLeaveStarted) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		slog.ErrorContext(ctx, "leave tournament handler failed", "tournament_id", id, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, LeaveTournamentResponse{
		Message:      "successfully left tournament",
		TournamentID: strconv.FormatInt(id, 10),
		UserID:       userID,
	})
}

func (h *Handler) StartTournament(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "user not authenticated",
		})
		return
	}

	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid tournament id",
		})
		return
	}

	ctx := c.Request.Context()
	tw, err := h.Service.StartTournament(ctx, id, userID)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "tournament not found",
			})
			return
		}

		if errors.Is(err, ErrNotCreator) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": err.Error(),
			})
			return
		}

		if errors.Is(err, ErrNotRegistration) || errors.Is(err, ErrNotEnoughPlayers) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		slog.ErrorContext(ctx, "start tournament handler failed", "tournament_id", id, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, StartTournamentResponse{
		Message:      "tournament started successfully",
		TournamentID: strconv.FormatInt(tw.ID, 10),
		Status:       tw.Status,
		CurrentRound: tw.CurrentRound,
	})
}

func (h *Handler) EndTournament(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "user not authenticated",
		})
		return
	}

	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid tournament id",
		})
		return
	}

	ctx := c.Request.Context()
	winnerID, err := h.Service.EndTournament(ctx, id, userID)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "tournament not found",
			})
			return
		}

		if errors.Is(err, ErrNotCreator) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": err.Error(),
			})
			return
		}

		if errors.Is(err, ErrNotOngoing) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		slog.ErrorContext(ctx, "end tournament handler failed", "tournament_id", id, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, EndTournamentResponse{
		Message:      "tournament ended successfully",
		TournamentID: strconv.FormatInt(id, 10),
		Status:       StatusCompleted,
		WinnerID:     winnerID,
	})
}

func (h *Handler) CancelTournament(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tournament id"})
		return
	}

	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx := c.Request.Context()
	err = h.Service.CancelTournament(ctx, id, userID)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "tournament not found",
			})
			return
		}

		if errors.Is(err, ErrNotCreator) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": err.Error(),
			})
			return
		}

		if errors.Is(err, ErrNotOngoing) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		slog.ErrorContext(ctx, "cancel tournament handler failed", "tournament_id", id, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, CancelTournamentResponse{
		Message:      "tournament cancelled successfully",
		TournamentID: strconv.FormatInt(id, 10),
		Status:       StatusCancelled,
	})
}

func (h *Handler) GetStandings(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tournament id"})
		return
	}

	ctx := c.Request.Context()
	res, err := h.Service.GetStandings(ctx, id)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "tournament not found"})
			return
		}
		slog.ErrorContext(ctx, "get standings handler failed", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetRounds(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tournament id"})
		return
	}

	var roundNumber []int
	if roundParam := c.Param("roundNumber"); roundParam != "" {
		rNum, err := strconv.Atoi(roundParam)
		if err == nil && rNum > 0 {
			roundNumber = append(roundNumber, rNum)
		}
	}

	ctx := c.Request.Context()
	res, err := h.Service.GetRounds(ctx, id, roundNumber...)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "tournament not found"})
			return
		}
		slog.ErrorContext(ctx, "get rounds handler failed", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetGames(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tournament id"})
		return
	}

	ctx := c.Request.Context()
	res, err := h.Service.GetGames(ctx, id)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "tournament not found"})
			return
		}
		slog.ErrorContext(ctx, "get games handler failed", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) GetMyGames(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tournament id"})
		return
	}

	ctx := c.Request.Context()
	res, err := h.Service.GetGames(ctx, id, userID)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "tournament not found"})
			return
		}
		slog.ErrorContext(ctx, "get my games handler failed", "id", id, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *Handler) RegenerateInviteCode(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "user not authenticated",
		})
		return
	}

	idParam := c.Param("id")
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid tournament id",
		})
		return
	}

	tw, err := h.Service.RegenerateInviteCode(c.Request.Context(), id, userID)
	if err != nil {
		if errors.Is(err, ErrTournamentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "tournament not found",
			})
			return
		}
		if errors.Is(err, ErrNotCreator) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": err.Error(),
			})
			return
		}

		slog.ErrorContext(c.Request.Context(), "regenerate invite code handler failed", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	res := ToTournamentDetailsResponse(tw)
	if tw.InviteCode != nil && *tw.InviteCode != "" {
		res.InviteCode = *tw.InviteCode
		res.Link = config.AppBaseURL + "/tournaments/invite/" + *tw.InviteCode
	}

	c.JSON(http.StatusOK, res)
}
