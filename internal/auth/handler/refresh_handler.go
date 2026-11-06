package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/dto"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/service"
	"github.com/here-arjun-1/Caisaara-backend/internal/response"
)

type RefreshHandler struct {
	RefreshService RefreshService
}

func NewRefreshHandler(
	refreshService RefreshService,
) *RefreshHandler {

	return &RefreshHandler{
		RefreshService: refreshService,
	}
}

func (h *RefreshHandler) Refresh(
	c *gin.Context,
) {
	var req dto.RefreshData
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		_ = c.ShouldBindJSON(&req)
	}

	if req.RefreshToken == "" {
		if tokenFromCookie, err := GetRefreshTokenFromCookie(c); err == nil && tokenFromCookie != "" {
			req.RefreshToken = tokenFromCookie
		}
	}

	req.RefreshToken = strings.TrimSpace(req.RefreshToken)
	if req.RefreshToken == "" {
		response.Error(c, http.StatusUnauthorized, "refresh token is required")
		return
	}

	accessToken, refreshToken, err := h.RefreshService.Refresh(c.Request.Context(), req)

	if errors.Is(err, service.ErrInternal) {
		response.Error(c, http.StatusInternalServerError, "internal server error")
		return
	}

	if errors.Is(err, service.ErrUserBanned) {
		response.Error(c, http.StatusForbidden, err.Error())
		return
	}

	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	SetAccessTokenCookie(c, accessToken)
	SetRefreshTokenCookie(c, refreshToken)

	response.Success(c, http.StatusOK, "token refreshed successfully", gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})
}
