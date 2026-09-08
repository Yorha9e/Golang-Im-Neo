package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang-im-neo-system/internal/middleware"
)

func newTestRouter(t *testing.T, svc *AuthService) (*gin.Engine, *TicketService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	tickets := NewTicketService()
	r := gin.New()
	rg := r.Group("/api/v1/auth")
	// Wire the real middleware so /ticket exercises the full chain.
	RegisterRoutes(rg, svc, tickets, middleware.JWTAuthMiddleware(testSecret, svc.db))
	return r, tickets
}

type envBody struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func doPOST(t *testing.T, r http.Handler, path, body, token string) (int, envBody) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env envBody
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("POST %s: bad envelope: %v (%s)", path, err, w.Body.String())
	}
	return w.Code, env
}

func TestHTTPFullFlow(t *testing.T) {
	svc, _ := newTestService(t)
	r, tickets := newTestRouter(t, svc)

	// register
	code, env := doPOST(t, r, "/api/v1/auth/register", `{"username":"alice","password":"secure_password"}`, "")
	if code != 200 || env.Code != 0 {
		t.Fatalf("register: http=%d env=%+v", code, env)
	}
	var reg struct {
		UserID   string `json:"user_id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(env.Data, &reg); err != nil || reg.UserID == "" || reg.Username != "alice" {
		t.Fatalf("register fields: %v %+v", err, reg)
	}

	// login
	code, env = doPOST(t, r, "/api/v1/auth/login",
		`{"username":"alice","password":"secure_password","device_class":"interactive","device_name":"Chrome"}`, "")
	if code != 200 || env.Code != 0 {
		t.Fatalf("login: http=%d env=%+v", code, env)
	}
	var login struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		UserID       string `json:"user_id"`
		SessionID    string `json:"session_id"`
	}
	if err := json.Unmarshal(env.Data, &login); err != nil {
		t.Fatal(err)
	}
	if login.AccessToken == "" || login.RefreshToken == "" || login.UserID != reg.UserID || login.SessionID == "" {
		t.Fatalf("login fields: %+v", login)
	}

	// refresh
	code, env = doPOST(t, r, "/api/v1/auth/refresh",
		`{"refresh_token":"`+login.RefreshToken+`","session_id":"`+login.SessionID+`"}`, "")
	if code != 200 || env.Code != 0 {
		t.Fatalf("refresh: http=%d env=%+v", code, env)
	}
	var ref struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(env.Data, &ref); err != nil || ref.AccessToken == "" || ref.RefreshToken == "" {
		t.Fatalf("refresh fields: %v %+v", err, ref)
	}
	// replay old RT via HTTP → 10002 with non-200 status
	code, env = doPOST(t, r, "/api/v1/auth/refresh",
		`{"refresh_token":"`+login.RefreshToken+`","session_id":"`+login.SessionID+`"}`, "")
	if env.Code != CodeUnauthorized || code == 200 {
		t.Fatalf("replay: http=%d env=%+v, want code 10002 non-200", code, env)
	}

	// ticket (protected) — use the rotated AT
	code, env = doPOST(t, r, "/api/v1/auth/ticket", `{}`, ref.AccessToken)
	if code != 200 || env.Code != 0 {
		t.Fatalf("ticket: http=%d env=%+v", code, env)
	}
	var tkt struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(env.Data, &tkt); err != nil || tkt.Ticket == "" {
		t.Fatalf("ticket fields: %v %+v", err, tkt)
	}
	uid, _, _, _, _, err := tickets.Redeem(tkt.Ticket)
	if err != nil || uid != reg.UserID {
		t.Fatalf("redeem http ticket: %v uid=%q", err, uid)
	}

	// ticket without auth → 401/10002
	code, env = doPOST(t, r, "/api/v1/auth/ticket", `{}`, "")
	if code != 401 || env.Code != CodeUnauthorized {
		t.Fatalf("ticket no-auth: http=%d env=%+v, want 401/10002", code, env)
	}
}

func TestHTTPBusinessErrors(t *testing.T) {
	svc, _ := newTestService(t)
	r, _ := newTestRouter(t, svc)

	if _, err := svc.Register("bob", "secure_password"); err != nil {
		t.Fatal(err)
	}
	// duplicate
	_, env := doPOST(t, r, "/api/v1/auth/register", `{"username":"bob","password":"secure_password"}`, "")
	if env.Code != CodeUserExisted {
		t.Fatalf("dup register: %+v, want 20002", env)
	}
	// wrong password
	_, env = doPOST(t, r, "/api/v1/auth/login", `{"username":"bob","password":"wrong_password"}`, "")
	if env.Code != CodePasswordIncorrect {
		t.Fatalf("wrong pw: %+v, want 20003", env)
	}
	// missing user
	_, env = doPOST(t, r, "/api/v1/auth/login", `{"username":"nobody","password":"secure_password"}`, "")
	if env.Code != CodePasswordIncorrect {
		t.Fatalf("missing user: %+v, want 20003", env)
	}
	// bad body
	code, env := doPOST(t, r, "/api/v1/auth/register", `not-json`, "")
	if code != 400 || env.Code != CodeParamInvalid {
		t.Fatalf("bad body: http=%d %+v, want 400/10001", code, env)
	}
}
