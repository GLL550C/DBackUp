package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	loginPassword string
	sessionStore  = make(map[string]time.Time)
	mu            sync.Mutex
)

// Init sets the login password
func Init(password string) {
	loginPassword = password
}

// LoginEndpoint handles POST /api/login
func LoginEndpoint(c *gin.Context) {
	var input struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if input.Password != loginPassword {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "密码错误"})
		return
	}

	// Create session token
	token := generateToken(input.Password)
	mu.Lock()
	sessionStore[token] = time.Now().Add(24 * time.Hour)
	mu.Unlock()

	// Set cookie (HttpOnly, SameSite)
	c.SetCookie("session", token, 86400, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "登录成功"})
}

// LogoutEndpoint handles POST /api/logout
func LogoutEndpoint(c *gin.Context) {
	token, _ := c.Cookie("session")
	if token != "" {
		mu.Lock()
		delete(sessionStore, token)
		mu.Unlock()
	}
	c.SetCookie("session", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "已退出"})
}

// Middleware checks authentication for protected routes
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Allow login page and login API
		path := c.Request.URL.Path
		if path == "/login" || path == "/api/login" {
			c.Next()
			return
		}

		// Allow static files
		if len(path) >= 8 && path[:8] == "/static/" {
			c.Next()
			return
		}

		// Check session cookie
		token, err := c.Cookie("session")
		if err != nil || token == "" {
			// If it's an API call, return 401 JSON
			if len(path) >= 5 && path[:5] == "/api/" {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			} else {
				c.Redirect(http.StatusFound, "/login")
			}
			c.Abort()
			return
		}

		mu.Lock()
		expires, ok := sessionStore[token]
		if !ok || time.Now().After(expires) {
			delete(sessionStore, token)
			mu.Unlock()
			if len(path) >= 5 && path[:5] == "/api/" {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "会话已过期"})
			} else {
				c.Redirect(http.StatusFound, "/login")
			}
			c.Abort()
			return
		}
		mu.Unlock()

		c.Next()
	}
}

func generateToken(password string) string {
	h := hmac.New(sha256.New, []byte(password))
	h.Write([]byte(time.Now().String()))
	return hex.EncodeToString(h.Sum(nil))
}

// CleanupExpired removes expired sessions (call periodically)
func CleanupExpired() {
	mu.Lock()
	defer mu.Unlock()
	now := time.Now()
	for token, expires := range sessionStore {
		if now.After(expires) {
			delete(sessionStore, token)
		}
	}
}
