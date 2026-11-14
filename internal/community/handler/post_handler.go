package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/here-arjun-1/Caisaara-backend/internal/community/dto"
	"github.com/here-arjun-1/Caisaara-backend/internal/community/service"
	"github.com/here-arjun-1/Caisaara-backend/internal/response"
)

type PostService interface {
	CreatePost(ctx context.Context, userID int64, req dto.CreatePostData) (*dto.PostResponse, error)
	GetPost(ctx context.Context, viewerID, postID int64) (*dto.PostResponse, error)
	GetFeed(ctx context.Context, viewerID, beforeID int64, limit int) (*dto.FeedResponse, error)
	DeletePost(ctx context.Context, userID, postID int64) error
	SetReaction(ctx context.Context, userID, postID int64, reaction string) (*dto.ReactionResponse, error)
	DeleteReaction(ctx context.Context, userID, postID int64) (*dto.ReactionResponse, error)
	GetBookmarks(ctx context.Context, userID, beforeID int64, limit int) (*dto.FeedResponse, error)
	AddBookmark(ctx context.Context, userID, postID int64) (*dto.BookmarkResponse, error)
	RemoveBookmark(ctx context.Context, userID, postID int64) (*dto.BookmarkResponse, error)
}

type PostHandler struct {
	PostService PostService
}

func NewPostHandler(postService PostService) *PostHandler {
	return &PostHandler{
		PostService: postService,
	}
}

func (h *PostHandler) CreatePost(c *gin.Context) {
	userID := c.GetInt64("user_id")
	if userID <= 0 {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)

	var req dto.CreatePostData
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}

	post, err := h.PostService.CreatePost(c.Request.Context(), userID, req)
	if err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusCreated, "post created successfully", post)
}

func (h *PostHandler) GetPost(c *gin.Context) {
	postID, ok := idParam(c, "id")
	if !ok {
		return
	}

	post, err := h.PostService.GetPost(c.Request.Context(), c.GetInt64("user_id"), postID)
	if err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "post fetched successfully", post)
}

func (h *PostHandler) GetFeed(c *gin.Context) {
	beforeID, ok := queryInt(c, "before_id")
	if !ok {
		return
	}
	limit, ok := queryInt(c, "limit")
	if !ok {
		return
	}

	feed, err := h.PostService.GetFeed(c.Request.Context(), c.GetInt64("user_id"), beforeID, int(limit))
	if err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "feed fetched successfully", feed)
}

func (h *PostHandler) DeletePost(c *gin.Context) {
	userID := c.GetInt64("user_id")
	if userID <= 0 {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	postID, ok := idParam(c, "id")
	if !ok {
		return
	}

	if err := h.PostService.DeletePost(c.Request.Context(), userID, postID); err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "post deleted successfully", nil)
}

func (h *PostHandler) SetReaction(c *gin.Context) {
	userID := c.GetInt64("user_id")
	if userID <= 0 {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	postID, ok := idParam(c, "id")
	if !ok {
		return
	}

	var req dto.SetReactionData
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.PostService.SetReaction(c.Request.Context(), userID, postID, req.Reaction)
	if err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "reaction saved", result)
}

func (h *PostHandler) DeleteReaction(c *gin.Context) {
	userID := c.GetInt64("user_id")
	if userID <= 0 {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	postID, ok := idParam(c, "id")
	if !ok {
		return
	}

	result, err := h.PostService.DeleteReaction(c.Request.Context(), userID, postID)
	if err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "reaction removed", result)
}

func (h *PostHandler) GetBookmarks(c *gin.Context) {
	userID := c.GetInt64("user_id")
	if userID <= 0 {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
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

	feed, err := h.PostService.GetBookmarks(c.Request.Context(), userID, beforeID, int(limit))
	if err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "bookmarks fetched successfully", feed)
}

func (h *PostHandler) AddBookmark(c *gin.Context) {
	userID := c.GetInt64("user_id")
	if userID <= 0 {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	postID, ok := idParam(c, "id")
	if !ok {
		return
	}

	result, err := h.PostService.AddBookmark(c.Request.Context(), userID, postID)
	if err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "post bookmarked", result)
}

func (h *PostHandler) RemoveBookmark(c *gin.Context) {
	userID := c.GetInt64("user_id")
	if userID <= 0 {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	postID, ok := idParam(c, "id")
	if !ok {
		return
	}

	result, err := h.PostService.RemoveBookmark(c.Request.Context(), userID, postID)
	if err != nil {
		writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "bookmark removed", result)
}

func idParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return id, true
}

func queryInt(c *gin.Context, name string) (int64, bool) {
	raw := c.Query(name)
	if raw == "" {
		return 0, true
	}

	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		response.Error(c, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return n, true
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInternal):
		response.Error(c, http.StatusInternalServerError, "internal server error")
	case errors.Is(err, service.ErrPostNotFound), errors.Is(err, service.ErrCommentNotFound), errors.Is(err, service.ErrGameNotFound), errors.Is(err, service.ErrUserNotFound):
		response.Error(c, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrNotAllowed), errors.Is(err, service.ErrUserBlocked):
		response.Error(c, http.StatusForbidden, err.Error())
	default:
		response.Error(c, http.StatusBadRequest, err.Error())
	}
}
