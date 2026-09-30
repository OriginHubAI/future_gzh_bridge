package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware requires Bearer token when authToken is non-empty.
func AuthMiddleware(authToken string) gin.HandlerFunc {
	authToken = strings.TrimSpace(authToken)
	return func(c *gin.Context) {
		if authToken == "" {
			c.Next()
			return
		}
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) || strings.TrimSpace(header[len(prefix):]) != authToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":  200001,
				"error": "unauthorized",
			})
			return
		}
		c.Next()
	}
}
