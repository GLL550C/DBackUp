package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware adds CORS headers for development
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// LoggerMiddleware logs each request with method, path, status, and latency
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)
		gin.DefaultWriter.Write([]byte(
			c.Request.Method + " " + c.Request.URL.Path + " " +
				http.StatusText(c.Writer.Status()) + " " +
				latency.String() + "\n",
		))
	}
}
