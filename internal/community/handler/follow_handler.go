package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/middleware"
	"github.com/here-arjun-1/Caisaara-backend/internal/community/dto"
	"github.com/here-arjun-1/Caisaara-backend/internal/response"
)

type FollowService interface {
	Follow(ctx context.Context, followerID, targetID int64) error
	Unfollow(ctx context.Context, followerID, targetID int64) error
	IsFollowing(ctx context.Context, followerID, targetID int64) (bool, error)
	GetCounts(ctx context.Context, userID int64) (*dto.FollowCountsResponse, error)
	GetFollowers(ctx context.Context, userID int64, beforeID int64, limit int) (*dto.UserListResponse, error)
	GetFollowing(ctx context.Context, userID int64, beforeID int64, limit int) (*dto.UserListResponse, error)
}

type FollowHandler struct {
	FollowService FollowService
}

func NewFollowHandler(followService FollowService) *FollowHandler {
	return &FollowHandler{
		FollowService: followService,
	}
}

func (h *FollowHandler) Follow(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	targetID, ok := idParam(c, "id")
	if !ok {
		return
	}

	if err := h.FollowService.Follow(c.Request.Context(), userID, targetID); err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "user followed successfully", dto.FollowStatusResponse{IsFollowing: true})
}

func (h *FollowHandler) Unfollow(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	targetID, ok := idParam(c, "id")
	if !ok {
		return
	}

	if err := h.FollowService.Unfollow(c.Request.Context(), userID, targetID); err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "user unfollowed successfully", dto.FollowStatusResponse{IsFollowing: false})
}

func (h *FollowHandler) GetFollowStatus(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID <= 0 {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	targetID, ok := idParam(c, "id")
	if !ok {
		return
	}

	isFollowing, err := h.FollowService.IsFollowing(c.Request.Context(), userID, targetID)
	if err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "follow status fetched successfully", dto.FollowStatusResponse{IsFollowing: isFollowing})
}

func (h *FollowHandler) GetCounts(c *gin.Context) {
	targetID, ok := idParam(c, "id")
	if !ok {
		return
	}

	counts, err := h.FollowService.GetCounts(c.Request.Context(), targetID)
	if err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "follow counts fetched successfully", counts)
}

func (h *FollowHandler) GetFollowers(c *gin.Context) {
	targetID, ok := idParam(c, "id")
	if !ok {
		return
	}

	beforeID, ok := queryInt(c, "before_id")
	if !ok {
		return
	}

	limit, ok := queryInt(c, "limit")
	if !ok {
		return
	}

	res, err := h.FollowService.GetFollowers(c.Request.Context(), targetID, beforeID, int(limit))
	if err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "followers fetched successfully", res)
}

func (h *FollowHandler) GetFollowing(c *gin.Context) {
	targetID, ok := idParam(c, "id")
	if !ok {
		return
	}

	beforeID, ok := queryInt(c, "before_id")
	if !ok {
		return
	}

	limit, ok := queryInt(c, "limit")
	if !ok {
		return
	}

	res, err := h.FollowService.GetFollowing(c.Request.Context(), targetID, beforeID, int(limit))
	if err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "following fetched successfully", res)
}

