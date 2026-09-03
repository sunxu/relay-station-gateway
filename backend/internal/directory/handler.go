package directory

import "github.com/gin-gonic/gin"

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Get(c *gin.Context) {
	if h == nil || h.service == nil {
		return
	}
	h.service.Serve(c)
}
