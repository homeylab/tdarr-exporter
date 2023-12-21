package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// http server structs
type InternalHealth struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// middleware to handle healthz
func HealthzHandler() gin.HandlerFunc {
	// define a function to handle this route using gin pkg context
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, InternalHealth{Status: "ok"})
	}
}
