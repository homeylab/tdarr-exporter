package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// middleware to handle index '/' route
func IndexHandler() gin.HandlerFunc {
	// define a function to handle this route using gin pkg context
	return func(c *gin.Context) {
		c.String(http.StatusOK, `<h1>tdarr-exporter</h1><p><a href='/metrics'>metrics</a></p>`)
	}
}
