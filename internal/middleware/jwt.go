package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/store"
)

// JWTAuthMiddleware validates Authorization: Bearer <token> and injects the
// identity into the gin context (string keys: user_id, username, role,
// session_id, device_class).
//
// Checks per request (Stage-1 M2):
//   - HS256 signature with explicit alg-confusion rejection, plus exp.
//   - DB user lookup by the user_id claim: missing → 401/{10002}.
//   - Status != 1 → 403/{20004} (banned).
//   - user.TokenVersion != claims tv → 401/{10002} (ban/kick lever).
func JWTAuthMiddleware(secret string, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "missing token"})
			return
		}
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "invalid token format"})
			return
		}
		tokenStr := strings.TrimSpace(parts[1])
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			// Explicit HS256 method check — reject alg confusion (e.g. none/RS256).
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method %q", t.Header["alg"])
			}
			if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, fmt.Errorf("unexpected signing alg %q", t.Header["alg"])
			}
			return []byte(secret), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "invalid or expired token"})
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "invalid claims"})
			return
		}
		// exp is mandatory: tokens without a valid future exp are rejected.
		expT, expErr := claims.GetExpirationTime()
		if expErr != nil || expT == nil || time.Now().After(expT.Time) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "invalid or expired token"})
			return
		}
		userID := claimString(claims, "user_id")
		if userID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "invalid claims"})
			return
		}

		var user store.User
		if err := db.Where("id = ?", userID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "user not found"})
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "auth check failed"})
			return
		}
		if user.Status != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 20004, "msg": "user account is banned"})
			return
		}
		tv, tvOK := claimInt(claims, "tv")
		if !tvOK || tv != user.TokenVersion {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "token revoked, please login again"})
			return
		}

		// Revoked-session check (M3, additive): when the token carries a
		// session_id, reject it if that session row exists AND is revoked.
		// An absent session row is allowed so pre-existing valid tokens keep
		// working. One indexed primary-key lookup.
		if sid := claimString(claims, "session_id"); sid != "" {
			var sess store.UserSession
			if err := db.Where("id = ?", sid).First(&sess).Error; err == nil && sess.IsRevoked != 0 {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 10002, "msg": "session revoked"})
				return
			}
		}

		c.Set("user_id", userID)
		c.Set("username", claimString(claims, "username"))
		c.Set("role", claimString(claims, "role"))
		c.Set("session_id", claimString(claims, "session_id"))
		c.Set("device_class", claimString(claims, "device_class"))
		c.Next()
	}
}

// AdminAuthMiddleware requires role == admin || superadmin.
func AdminAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get("role")
		if role != "admin" && role != "superadmin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 10003, "msg": "forbidden: admin only"})
			return
		}
		c.Next()
	}
}

// claimString coerces a MapClaims value to string (claims decode as
// interface{}; non-string values are stringified so context stays typed).
func claimString(claims jwt.MapClaims, key string) string {
	v, ok := claims[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// claimInt extracts an integer claim (JSON numbers decode as float64).
func claimInt(claims jwt.MapClaims, key string) (int, bool) {
	v, ok := claims[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	default:
		return 0, false
	}
}
