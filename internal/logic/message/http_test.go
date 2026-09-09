package message

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/logic/session"
	"golang-im-neo-system/internal/middleware"
	"golang-im-neo-system/internal/store"
)

const msgTestSecret = "msg-test-secret-32chars-long!!"

func signMsgToken(t *testing.T, secret, uid, username string, tv int) string {
	t.Helper()
	claims := jwt.MapClaims{
		"user_id":      uid,
		"username":     username,
		"tv":           tv,
		"role":         "user",
		"device_class": "interactive",
		"session_id":   "s_test_" + uid,
		"exp":          time.Now().Add(2 * time.Hour).Unix(),
		"iat":          time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return s
}

func newMsgHTTPSetup(t *testing.T) (*gin.Engine, *gorm.DB, *MessageService, map[string]string, map[string]string) {
	t.Helper()
	db, err := store.NewDB(store.DBConfig{Path: filepath.Join(t.TempDir(), "msg_test.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})

	for _, u := range []struct{ id, name string }{
		{"u_alice", "alice"},
		{"u_bob", "bob"},
		{"u_carol", "carol"},
	} {
		if err := db.Create(&store.User{
			ID: u.id, Username: u.name, PasswordHash: "hash",
			Role: "user", Status: 1, TokenVersion: 1,
		}).Error; err != nil {
			t.Fatalf("seed %q: %v", u.name, err)
		}
	}

	// Seed mutual friendship between Alice and Bob
	now := time.Now()
	for _, pair := range [][2]string{{"u_alice", "u_bob"}, {"u_bob", "u_alice"}} {
		if err := db.Create(&store.Friendship{
			UserID: pair[0], FriendID: pair[1], InitiatorID: "u_alice",
			Status: "accepted", CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed friendship: %v", err)
		}
	}

	svc := NewMessageService(db, nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	rg := r.Group("/api/v1/messages")
	RegisterRoutes(rg, svc, middleware.JWTAuthMiddleware(msgTestSecret, db))

	tokens := map[string]string{}
	ids := map[string]string{"alice": "u_alice", "bob": "u_bob", "carol": "u_carol"}
	for name, uid := range ids {
		tokens[name] = signMsgToken(t, msgTestSecret, uid, name, 1)
	}
	return r, db, svc, tokens, ids
}

func seedMsg(t *testing.T, db *gorm.DB, covID string, seq int64, from, to, content string, ts int64) {
	t.Helper()
	m := store.Message{
		CovID:       covID,
		Seq:         seq,
		StanzaID:    fmt.Sprintf("stz_%s_%d", covID, seq),
		FromUID:     from,
		ToUID:       to,
		Content:     content,
		Timestamp:   ts,
		ContentType: 1,
		Status:      1,
		CreatedAt:   time.UnixMilli(ts),
	}
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("seed msg cov=%s seq=%d: %v", covID, seq, err)
	}
}

type httpEnv struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func doReq(r *gin.Engine, method, path, token string) (int, httpEnv) {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env httpEnv
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	return w.Code, env
}

func TestPublicHallHistory(t *testing.T) {
	r, db, _, tokens, _ := newMsgHTTPSetup(t)

	// Seed 5 hall messages
	for i := int64(1); i <= 5; i++ {
		seedMsg(t, db, PublicHallCovID, i, "u_alice", "", fmt.Sprintf("hall msg %d", i), 1000+i*10)
	}

	// 1. Query hall history via canonical cov_id
	code, env := doReq(r, http.MethodGet, "/api/v1/messages/history?cov_id=cov:public:hall", tokens["alice"])
	if code != http.StatusOK || env.Code != 0 {
		t.Fatalf("expected 200/0, got http=%d code=%d msg=%s", code, env.Code, env.Msg)
	}
	var res HistoryResult
	if err := json.Unmarshal(env.Data, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(res.Messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(res.Messages))
	}
	// Verify ascending seq order
	for i, m := range res.Messages {
		if m.Seq != int64(i+1) {
			t.Errorf("msg[%d] seq = %d, want %d", i, m.Seq, i+1)
		}
	}
	// Sender profile hydration: hall messages must carry from_username/from_role.
	for i, m := range res.Messages {
		if m.FromUID != "u_alice" {
			t.Errorf("msg[%d] from_uid = %q, want u_alice", i, m.FromUID)
		}
		if m.FromUsername != "alice" {
			t.Errorf("msg[%d] from_username = %q, want alice", i, m.FromUsername)
		}
		if m.FromRole != "user" {
			t.Errorf("msg[%d] from_role = %q, want user", i, m.FromRole)
		}
	}

	// 2. Query via alias route
	code, env = doReq(r, http.MethodGet, "/api/v1/messages/hall/history", tokens["bob"])
	if code != http.StatusOK || env.Code != 0 {
		t.Fatalf("alias hall history failed: http=%d code=%d", code, env.Code)
	}
	var aliasRes HistoryResult
	if err := json.Unmarshal(env.Data, &aliasRes); err != nil {
		t.Fatalf("unmarshal alias hall: %v", err)
	}
	if len(aliasRes.Messages) != 5 {
		t.Fatalf("alias hall: expected 5 messages, got %d", len(aliasRes.Messages))
	}
	if aliasRes.Messages[0].FromUsername != "alice" || aliasRes.Messages[0].FromRole != "user" {
		t.Errorf("alias hall sender hydration missing: %+v", aliasRes.Messages[0])
	}

	// 3. Query with before_seq pagination
	code, env = doReq(r, http.MethodGet, "/api/v1/messages/history?cov_id=cov:public:hall&before_seq=4&limit=2", tokens["carol"])
	if code != http.StatusOK || env.Code != 0 {
		t.Fatalf("pagination failed: http=%d", code)
	}
	_ = json.Unmarshal(env.Data, &res)
	if len(res.Messages) != 2 || res.Messages[0].Seq != 2 || res.Messages[1].Seq != 3 {
		t.Fatalf("expected seq 2,3, got %+v", res.Messages)
	}
	if !res.HasMore {
		t.Errorf("expected has_more=true")
	}
}

func TestPrivateChatHistory(t *testing.T) {
	r, db, _, tokens, ids := newMsgHTTPSetup(t)

	covID := session.BuildPrivateCovID(ids["alice"], ids["bob"])

	// Seed private messages between Alice and Bob
	for i := int64(1); i <= 3; i++ {
		seedMsg(t, db, covID, i, ids["alice"], ids["bob"], fmt.Sprintf("private %d", i), 2000+i*10)
	}

	// 1. Alice queries history -> 200
	code, env := doReq(r, http.MethodGet, "/api/v1/messages/history?cov_id="+covID, tokens["alice"])
	if code != http.StatusOK || env.Code != 0 {
		t.Fatalf("Alice query failed: http=%d code=%d", code, env.Code)
	}
	var res HistoryResult
	_ = json.Unmarshal(env.Data, &res)
	if len(res.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(res.Messages))
	}
	// Sender profile hydration for private history.
	for i, m := range res.Messages {
		if m.FromUsername != "alice" {
			t.Errorf("private msg[%d] from_username = %q, want alice", i, m.FromUsername)
		}
		if m.FromRole != "user" {
			t.Errorf("private msg[%d] from_role = %q, want user", i, m.FromRole)
		}
	}

	// 2. Bob queries via alias -> 200
	code, env = doReq(r, http.MethodGet, "/api/v1/messages/private/"+ids["alice"]+"/history", tokens["bob"])
	if code != http.StatusOK || env.Code != 0 {
		t.Fatalf("Bob alias query failed: http=%d code=%d", code, env.Code)
	}
	var aliasRes HistoryResult
	_ = json.Unmarshal(env.Data, &aliasRes)
	if len(aliasRes.Messages) != 3 {
		t.Fatalf("alias private: expected 3 messages, got %d", len(aliasRes.Messages))
	}
	if aliasRes.Messages[0].FromUsername != "alice" || aliasRes.Messages[0].FromRole != "user" {
		t.Errorf("alias private sender hydration missing: %+v", aliasRes.Messages[0])
	}

	// 3. Carol (not participant) queries Alice-Bob history -> 403 Forbidden
	code, env = doReq(r, http.MethodGet, "/api/v1/messages/history?cov_id="+covID, tokens["carol"])
	if code != http.StatusForbidden || env.Code != CodeForbidden {
		t.Fatalf("Carol non-participant should be 403/10003, got http=%d code=%d", code, env.Code)
	}

	// 4. Alice queries history with Carol (not friends) -> 403 / 30001
	carolCov := session.BuildPrivateCovID(ids["alice"], ids["carol"])
	code, env = doReq(r, http.MethodGet, "/api/v1/messages/history?cov_id="+carolCov, tokens["alice"])
	if code != http.StatusForbidden || env.Code != CodeFriendNotFound {
		t.Fatalf("Alice-Carol non-friends should be 403/30001, got http=%d code=%d", code, env.Code)
	}
}

func TestMessageHistoryValidation(t *testing.T) {
	r, _, _, tokens, _ := newMsgHTTPSetup(t)

	// Missing cov_id
	code, env := doReq(r, http.MethodGet, "/api/v1/messages/history", tokens["alice"])
	if code != http.StatusBadRequest || env.Code != CodeParamInvalid {
		t.Errorf("missing cov_id should be 400/10001, got http=%d code=%d", code, env.Code)
	}

	// Unauthenticated
	code, env = doReq(r, http.MethodGet, "/api/v1/messages/history?cov_id=cov:public:hall", "")
	if code != http.StatusUnauthorized {
		t.Errorf("unauthenticated should be 401, got http=%d", code)
	}
}

func TestGroupChatHistoryAuthz(t *testing.T) {
	r, db, _, tokens, ids := newMsgHTTPSetup(t)

	// Seed a group and group members
	grpID := "grp_test_authz"
	now := time.Now()
	if err := db.Create(&store.Group{
		ID: grpID, Name: "Secret Group", OwnerID: ids["alice"], CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	for _, uid := range []string{ids["alice"], ids["bob"]} {
		if err := db.Create(&store.GroupMember{
			GroupID: grpID, UserID: uid, Role: "member", JoinedVia: "create", CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed member %s: %v", uid, err)
		}
	}

	covID := "cov:grp:" + grpID
	seedMsg(t, db, covID, 1, ids["alice"], grpID, "secret group message", 5000)

	// 1. Alice (member) queries group history -> 200
	code, env := doReq(r, http.MethodGet, "/api/v1/messages/history?cov_id="+covID, tokens["alice"])
	if code != http.StatusOK || env.Code != 0 {
		t.Fatalf("member Alice should succeed, got http=%d code=%d", code, env.Code)
	}

	// 2. Carol (non-member) queries group history -> 403 Forbidden / 40002
	code, env = doReq(r, http.MethodGet, "/api/v1/messages/history?cov_id="+covID, tokens["carol"])
	if code != http.StatusForbidden || env.Code != CodeGroupNotMember {
		t.Fatalf("non-member Carol should be 403/40002, got http=%d code=%d", code, env.Code)
	}
}
