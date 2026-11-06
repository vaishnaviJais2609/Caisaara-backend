package bot

import (
	"github.com/gin-gonic/gin"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/middleware"
)

func RegisterRoutes(handler *Handler, r *gin.Engine, protected *gin.RouterGroup) {
	botGames := protected.Group("/bot-games")
	botGames.Use(middleware.RequireRegisteredUser())

	botGames.POST("", handler.CreateGame)
	botGames.GET("/:gameID", handler.GetGame)
	botGames.GET("", handler.ListGames)
	botGames.GET("/bots", handler.ListBots)
	botGames.POST("/:gameID/resign", handler.ResignGame)

	r.GET("/ws/bot-games/:gameID", handler.Connect)
}
