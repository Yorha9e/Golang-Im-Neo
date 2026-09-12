package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"golang-im-neo-system/internal/middleware"
)

func TestCORSMiddleware_SameOriginAndWhitelist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.CORSMiddleware())
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	// 1. Same-origin request
	req := httptest.NewRequest(http.MethodGet, "http://106.52.170.56:8080/ping", nil)
	req.Header.Set("Origin", "http://106.52.170.56:8080")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://106.52.170.56:8080" {
		t.Fatalf("expected same-origin to be allowed")
	}

	// 2. Whitelist origin (localhost:5173)
	req = httptest.NewRequest(http.MethodGet, "http://106.52.170.56:8080/ping", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("expected localhost origin to be allowed")
	}
}

func TestCORSMiddleware_UntrustedOriginRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.CORSMiddleware())
	r.OPTIONS("/ping", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	// 1. Untrusted origin preflight (OPTIONS) -> 403 Forbidden
	req := httptest.NewRequest(http.MethodOptions, "http://106.52.170.56:8080/ping", nil)
	req.Header.Set("Origin", "https://evil-attacker.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected untrusted origin preflight to be rejected with 403, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("untrusted origin must not have Access-Control-Allow-Origin header")
	}

	// 2. Untrusted origin GET request -> no Access-Control-Allow-Origin header
	req = httptest.NewRequest(http.MethodGet, "http://106.52.170.56:8080/ping", nil)
	req.Header.Set("Origin", "https://evil-attacker.com")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("untrusted origin GET request must not reflect Access-Control-Allow-Origin")
	}
}
