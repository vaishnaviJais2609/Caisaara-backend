package invitation

import "github.com/gin-gonic/gin"

func RegisterRoutes(handler *Handler, public *gin.Engine, protected *gin.RouterGroup) {
	protected.POST("/invite", handler.Create)
	protected.POST("/invite/:code/join", handler.Join)
	public.GET("/invite/:code", handler.Preview)
}
