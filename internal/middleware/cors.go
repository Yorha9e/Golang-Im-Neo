package middleware

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// [LAN-DEBUG-FLAG] CORSMiddleware enables cross-origin resource sharing with safe origin whitelist.
// Local & LAN multi-device debugging toggle:
// To allow arbitrary private LAN origins (10.x, 192.168.x), uncomment the LAN block inside isAllowedOrigin.
// Disabled by default for production & security penetration testing.
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			if isAllowedOrigin(origin, c.Request.Host) {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
				c.Header("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, PATCH, DELETE")
				c.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers, Content-Type")
			} else if c.Request.Method == http.MethodOptions {
				// Reject untrusted cross-origin preflight immediately
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func isAllowedOrigin(origin, host string) bool {
	if origin == "" {
		return true
	}
	ou, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := strings.ToLower(ou.Hostname())
	if originHost == "" {
		return false
	}

	// 1. Same-origin (Nginx reverse-proxy or direct match)
	reqHost := strings.ToLower(requestHostname(host))
	if originHost == reqHost {
		return true
	}

	// 2. Safe local development and mobile webview origins (localhost, 127.0.0.1, etc.)
	switch originHost {
	case "localhost", "127.0.0.1", "::1":
		return true
	}

	// [LAN-DEBUG-FLAG] Local & LAN multi-device debugging toggle:
	// To re-enable LAN/private IP wildcarding (10.x, 172.16-31.x, 192.168.x) for offline testing,
	// simply uncomment the block below. Disabled by default for production & security penetration testing.
	/*
	if ip := net.ParseIP(originHost); ip != nil {
		if ip.IsPrivate() || ip.IsLoopback() {
			return true
		}
	}
	*/

	return false
}

func requestHostname(hostPort string) string {
	if hostPort == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		return h
	}
	return strings.Trim(hostPort, "[]")
}
