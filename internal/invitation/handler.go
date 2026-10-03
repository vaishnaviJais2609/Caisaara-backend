package invitation

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	Service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		Service: service,
	}
}

type CreateInviteRequest struct {
	TimeControlMinutes int    `json:"time_control_minutes" binding:"required"`
	Color              string `json:"color" binding:"required"`
}

func (h *Handler) Create(c *gin.Context) {

	userIDValue, exists := c.Get("user_id")

	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "user not authenticated",
		})
		return
	}

	userID, ok := userIDValue.(int64)

	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "invalid user id",
		})
		return
	}

	var req CreateInviteRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	invite, err := h.Service.CreateInvite(
		c.Request.Context(),
		userID,
		req.TimeControlMinutes,
		strings.ToLower(req.Color),
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"invite_code":          invite.Code,
		"link":                 "https://caisaara.app/play/" + invite.Code,
		"time_control_minutes": invite.TimeControlMinutes,
		"color":                invite.Color,
		"status":               "waiting",
	})
}
func (h *Handler) Preview(c *gin.Context) {

	code := c.Param("code")

	invite, err := h.Service.GetInvite(
		c.Request.Context(),
		code,
	)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "invite not found or expired",
		})
		return
	}

	user, err := h.Service.UserRepository.FindUserByID(
		invite.CreatorID,
	)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "inviter not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"inviter": gin.H{
			"id":       user.ID,
			"username": user.Username,
		},
		"time_control_minutes": invite.TimeControlMinutes,
		"color":                invite.Color,
		"status":               "waiting",
	})
}

func (h *Handler) Join(c *gin.Context) {

	userIDValue, exists := c.Get("user_id")

	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "user not authenticated",
		})
		return
	}

	userID, ok := userIDValue.(int64)

	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "invalid user id",
		})
		return
	}

	code := c.Param("code")

	gameID, err := h.Service.JoinInvite(
		c.Request.Context(),
		code,
		userID,
	)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"game_id": gameID,
		"status":  "active",
	})
}
