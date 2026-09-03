package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/directory"
	"github.com/gin-gonic/gin"
)

func RegisterDirectoryRoutes(r *gin.Engine, h *directory.Handler) {
	if h == nil {
		return
	}
	internal := r.Group("/internal/v1")
	internal.Any("/api-account-directory", h.Get)
}
