package router

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/middleware"
	"github.com/here-arjun-1/Caisaara-backend/internal/community"
	"github.com/here-arjun-1/Caisaara-backend/internal/player"
	"github.com/here-arjun-1/Caisaara-backend/internal/response"
)

func New(jwtSecret string, authModule *auth.Module, playerModule *player.Module, communityModule *community.Module) (*gin.Engine, error) {
	r := gin.Default()

	if err := r.SetTrustedProxies([]string{"127.0.0.1", "::1", "172.16.0.0/12"}); err != nil {
		return nil, fmt.Errorf("set trusted proxies: %w", err)
	}

	r.GET("/health", func(c *gin.Context) {
		response.Success(c, http.StatusOK, "ok", nil)
	})

	protected := r.Group("/api")
	protected.Use(middleware.JWTMiddleware(jwtSecret, authModule.UserRepository))

	public := r.Group("")
	public.Use(middleware.OptionalJWTMiddleware(jwtSecret))

	authModule.RegisterRoutes(r, protected)
	playerModule.RegisterRoutes(r, protected)
	communityModule.RegisterRoutes(public, protected)

	return r, nil
}
