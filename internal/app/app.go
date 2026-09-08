// Package app wires the Stage-1 layers (store/auth/handshake+gateway/
// router) plus the Stage-2 services (friend/media/admin/profile) and the Stage-4
// group service into one runnable server (MISSIONS M5 + M4, Stage-4 M1/M2/M3).
//
// Layer flow (docs/DECOUPLED_ARCHITECTURE_SPEC.md):
//
//	HTTP register/login/ticket -> auth.AuthService + auth.TicketService
//	WS /ws?ticket=...          -> handshake.Handler (redeem-then-upgrade)
//	frames                     -> gateway.Hub -> router.Router (interceptor
//	                            chain -> CentralRelayStrategy -> store.BatchWriter)
//
// Config load stays OUTSIDE: the caller passes a loaded *config.Config and
// Build owns logger, DB open, superadmin seed, allocator, batchWriter, hub,
// router, handshake and route registration.
package app

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"golang-im-neo-system/internal/config"
	"golang-im-neo-system/internal/gateway"
	"golang-im-neo-system/internal/handshake"
	"golang-im-neo-system/internal/logic/admin"
	"golang-im-neo-system/internal/logic/auth"
	"golang-im-neo-system/internal/logic/friend"
	"golang-im-neo-system/internal/logic/group"
	"golang-im-neo-system/internal/logic/media"
	"golang-im-neo-system/internal/logic/message"
	"golang-im-neo-system/internal/logic/profile"
	"golang-im-neo-system/internal/logic/session"
	"golang-im-neo-system/internal/logic/user"
	"golang-im-neo-system/internal/middleware"
	"golang-im-neo-system/internal/router"
	"golang-im-neo-system/internal/store"
	"golang-im-neo-system/pkg/logger"
)

// App is the fully-wired Stage-1 server. Engine is served by cmd/server;
// Hub/DB are exported so integration tests can drive real WS clients and
// seed rows directly.
type App struct {
	Config *config.Config
	Logger *zap.Logger
	DB     *gorm.DB

	Batch   *store.BatchWriter
	Alloc   *session.Allocator
	Auth    *auth.AuthService
	Tickets *auth.TicketService
	Friend  *friend.FriendService
	Media   *media.MediaService
	Admin   *admin.AdminService
	Profile *profile.ProfileService
	Group   *group.GroupService
	Message *message.MessageService
	User    *user.UserService
	Hub     *gateway.Hub
	Router  *router.Router
	Engine  *gin.Engine
}

// Build wires every layer per the M5 contract and returns a ready-to-serve App.
func Build(cfg *config.Config) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("app: nil config")
	}

	zapLogger, err := logger.New(cfg.Server.Mode)
	if err != nil {
		return nil, fmt.Errorf("app: init logger: %w", err)
	}

	db, err := cfg.OpenDB()
	if err != nil {
		return nil, fmt.Errorf("app: open db: %w", err)
	}

	if err := config.SeedSuperAdmin(db, cfg); err != nil {
		closeDB(db)
		return nil, fmt.Errorf("app: seed superadmin: %w", err)
	}

	batchWriter := store.NewBatchWriter(db, zapLogger)
	batchWriter.Start()

	allocator := session.NewAllocator(db)
	if err := allocator.LoadAll(); err != nil {
		// Warn-non-fatal: lazy load on first Allocate covers a cold start.
		zapLogger.Warn("app: allocator preload failed (non-fatal)", zap.Error(err))
	}

	authSvc := auth.NewAuthService(db, cfg)
	tickets := auth.NewTicketService()
	jwtMW := middleware.JWTAuthMiddleware(cfg.Security.JWTSecret, db)
	adminMW := middleware.AdminAuthMiddleware()

	hub := gateway.NewHub(zapLogger)
	go hub.Run()

	rtr := router.New(hub /*Emitter*/, batchWriter /*Persister*/, allocator, db, zapLogger)
	hub.SetInboundHandler(rtr) // *router.Router satisfies gateway.InboundHandler

	// Stage-2 services (M1/M2/M3): *gateway.Hub satisfies friend.Emitter,
	// *router.Router satisfies friend.Invalidator.
	friendSvc := friend.NewFriendService(db, hub, rtr, zapLogger)
	mediaSvc := media.NewMediaService(db, cfg, zapLogger)
	adminSvc := admin.NewAdminService(db, hub, batchWriter, zapLogger)
	profileSvc := profile.NewProfileService(db, nil, zapLogger)
	groupSvc := group.NewGroupService(db, rtr, zapLogger)
	messageSvc := message.NewMessageService(db, zapLogger)
	userSvc := user.NewUserService(db, zapLogger)

	hs := handshake.NewHandler(hub, tickets /*TicketVerifier*/, zapLogger, nil /*secure default origin*/)

	setGinMode(cfg.Server.Mode)
	engine := gin.New()
	// [LAN-DEBUG-FLAG] Enable CORS middleware for LAN and separate frontend ports (5173/3000)
	engine.Use(gin.Recovery(), middleware.CORSMiddleware())
	auth.RegisterRoutes(engine.Group("/api/v1/auth"), authSvc, tickets, jwtMW)
	friend.RegisterRoutes(engine.Group("/api/v1/friends"), friendSvc, jwtMW)
	group.RegisterRoutes(engine.Group("/api/v1/groups"), groupSvc, jwtMW)
	media.RegisterRoutes(engine.Group("/api/v1/media"), mediaSvc, jwtMW)
	profile.RegisterRoutes(engine.Group("/api/v1/profile"), engine.Group("/api/v1/posts"), profileSvc, jwtMW)
	message.RegisterRoutes(engine.Group("/api/v1/messages"), messageSvc, jwtMW)
	admin.RegisterRoutes(engine.Group("/api/v1/admin"), adminSvc, jwtMW, adminMW)
	user.RegisterRoutes(engine.Group("/api/v1/users"), userSvc, jwtMW)
	engine.GET("/ws", hs.ServeWS)

	// Public root health probe (no auth): {"code":0,"msg":"success","data":{"status":"ok","uptime_seconds":...}}.
	buildTime := time.Now()
	engine.GET("/health", func(c *gin.Context) {
		uptime := int64(time.Since(buildTime).Seconds())
		if uptime < 0 {
			uptime = 0
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"status": "ok", "uptime_seconds": uptime}})
	})

	// Built-in Web Console
	engine.StaticFile("/", "./web/index.html")
	engine.Static("/web", "./web")

	zapLogger.Info("app wired",
		zap.String("addr", cfg.Addr()),
		zap.String("db", cfg.Database.Path))

	return &App{
		Config: cfg, Logger: zapLogger, DB: db,
		Batch: batchWriter, Alloc: allocator,
		Auth: authSvc, Tickets: tickets,
		Friend: friendSvc, Media: mediaSvc, Admin: adminSvc, Profile: profileSvc, Group: groupSvc, Message: messageSvc, User: userSvc,
		Hub: hub, Router: rtr, Engine: engine,
	}, nil
}

// Close shuts the app down gracefully: it stops the batch writer (drain-flush
// so every queued message is persisted and every sync waiter gets its result)
// and then closes the DB handle. Hub connections just die with the process —
// there is no cross-process session migration in Stage-1; clients reconnect
// with a fresh ticket and stanza-id retries dedup server-side.
func (a *App) Close() error {
	if a == nil {
		return nil
	}
	if a.Tickets != nil {
		a.Tickets.Stop()
	}
	if a.Batch != nil {
		a.Batch.Stop()
	}
	if a.DB != nil {
		if sqlDB, err := a.DB.DB(); err == nil && sqlDB != nil {
			if err := sqlDB.Close(); err != nil {
				return fmt.Errorf("app: close db: %w", err)
			}
		}
	}
	if a.Logger != nil {
		_ = a.Logger.Sync()
	}
	return nil
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil && sqlDB != nil {
		_ = sqlDB.Close()
	}
}

// setGinMode maps the config server.mode to a gin mode. Unknown values fall
// back to debug; "test" keeps test output quiet.
func setGinMode(mode string) {
	switch mode {
	case "release", "production":
		gin.SetMode(gin.ReleaseMode)
	case "test":
		gin.SetMode(gin.TestMode)
	default:
		gin.SetMode(gin.DebugMode)
	}
}
