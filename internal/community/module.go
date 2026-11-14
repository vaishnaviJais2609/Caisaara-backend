package community

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/middleware"
	"github.com/here-arjun-1/Caisaara-backend/internal/community/handler"
	"github.com/here-arjun-1/Caisaara-backend/internal/community/repository"
	"github.com/here-arjun-1/Caisaara-backend/internal/community/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Module struct {
	postService    *service.PostService
	commentService *service.CommentService
	followService  *service.FollowService
	postHandler    *handler.PostHandler
	commentHandler *handler.CommentHandler
	followHandler  *handler.FollowHandler
}

func NewModule(db *pgxpool.Pool) *Module {
	postRepository := repository.NewPostRepository(db)
	commentRepository := repository.NewCommentRepository(db)
	gameRepository := repository.NewGameRepository(db)
	followRepository := repository.NewFollowRepository(db)

	postService := service.NewPostService(postRepository, gameRepository)
	commentService := service.NewCommentService(commentRepository, postRepository)
	followService := service.NewFollowService(followRepository)

	return &Module{
		postService:    postService,
		commentService: commentService,
		followService:  followService,
		postHandler:    handler.NewPostHandler(postService),
		commentHandler: handler.NewCommentHandler(commentService),
		followHandler:  handler.NewFollowHandler(followService),
	}
}

func (m *Module) SetNotifier(notifier service.Notifier) {
	m.postService.SetNotifier(notifier)
	m.commentService.SetNotifier(notifier)
	m.followService.SetNotifier(notifier)
}

func (m *Module) RegisterRoutes(public *gin.RouterGroup, protected *gin.RouterGroup) {
	readLimiter := middleware.NewTokenBucketLimiter(60, time.Minute)
	createPostLimiter := middleware.NewTokenBucketLimiter(5, time.Minute)
	commentLimiter := middleware.NewTokenBucketLimiter(20, time.Minute)
	reactionLimiter := middleware.NewTokenBucketLimiter(30, time.Minute)
	deleteLimiter := middleware.NewTokenBucketLimiter(20, time.Minute)
	bookmarkLimiter := middleware.NewTokenBucketLimiter(30, time.Minute)
	followLimiter := middleware.NewTokenBucketLimiter(30, time.Minute)

	public.GET("/posts", readLimiter.Limit, m.postHandler.GetFeed)
	public.GET("/posts/:id", readLimiter.Limit, m.postHandler.GetPost)
	public.GET("/posts/:id/comments", readLimiter.Limit, m.commentHandler.ListComments)

	registered := protected.Group("")
	registered.Use(middleware.RequireRegisteredUser())
	{
		registered.POST("/posts", createPostLimiter.Limit, m.postHandler.CreatePost)
		registered.DELETE("/posts/:id", deleteLimiter.Limit, m.postHandler.DeletePost)

		registered.PUT("/posts/:id/reaction", reactionLimiter.Limit, m.postHandler.SetReaction)
		registered.DELETE("/posts/:id/reaction", reactionLimiter.Limit, m.postHandler.DeleteReaction)
		registered.POST("/posts/:id/comments", commentLimiter.Limit, m.commentHandler.CreateComment)
		registered.DELETE("/comments/:id", deleteLimiter.Limit, m.commentHandler.DeleteComment)

		registered.GET("/bookmarks", readLimiter.Limit, m.postHandler.GetBookmarks)
		registered.PUT("/posts/:id/bookmark", bookmarkLimiter.Limit, m.postHandler.AddBookmark)
		registered.DELETE("/posts/:id/bookmark", bookmarkLimiter.Limit, m.postHandler.RemoveBookmark)

		registered.PUT("/users/:id/follow", followLimiter.Limit, m.followHandler.Follow)
		registered.POST("/users/:id/follow", followLimiter.Limit, m.followHandler.Follow)
		registered.DELETE("/users/:id/follow", followLimiter.Limit, m.followHandler.Unfollow)
		registered.GET("/users/:id/follow-status", followLimiter.Limit, m.followHandler.GetFollowStatus)
		registered.GET("/users/:id/follow-counts", followLimiter.Limit, m.followHandler.GetCounts)
		registered.GET("/users/:id/followers", followLimiter.Limit, m.followHandler.GetFollowers)
		registered.GET("/users/:id/following", followLimiter.Limit, m.followHandler.GetFollowing)
	}
}
