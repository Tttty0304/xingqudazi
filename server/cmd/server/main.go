// Package main 是兴趣搭子在线聊天室后端服务的启动入口。
// Task1 接线基础设施（配置/日志/DB/Redis/健康检查/优雅关闭）；
// Task2 起接入用户体系与鉴权（注册/登录/访客模式/JWT）。
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/api"
	"xingqudazi-im/server/internal/config"
	"xingqudazi-im/server/internal/middleware"
	"xingqudazi-im/server/internal/repository"
	"xingqudazi-im/server/internal/service"
	"xingqudazi-im/server/internal/ws"
	logger "xingqudazi-im/server/pkg/log"
)

// instanceID 标识当前进程实例（能力补齐项：支撑多实例部署下的可观测性与
// 更丰富的部署/运行时测试，见 api.HealthHandler.InstanceID /
// ws.ServerMessage.InstanceID 注释）。优先读取 `INSTANCE_ID` 环境变量
// （方便在非容器环境/测试中显式指定一个好记的名字），否则回落到
// `os.Hostname()`（Docker 默认把容器 ID 短哈希设为 hostname，同一个
// docker-compose 服务的不同副本天然拥有不同的 hostname，无需额外配置）。
func instanceID() string {
	if v := os.Getenv("INSTANCE_ID"); v != "" {
		return v
	}
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

func main() {
	log := logger.New()

	cfg, err := config.Load()
	if err != nil {
		log.Error("load config failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	selfInstanceID := instanceID()
	log.Info("instance starting", "instance_id", selfInstanceID)

	dbPool, err := repository.NewPostgresPool(ctx, cfg.PostgresDSN, cfg.PostgresMaxConns, cfg.PostgresMinConns)
	if err != nil {
		// 启动期依赖不可用：明确失败退出，而不是"看起来启动成功但实际半残"。
		log.Error("connect postgres failed", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()
	log.Info("postgres connected")

	redisClient, err := repository.NewRedisClient(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		log.Error("connect redis failed", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()
	log.Info("redis connected")

	router := gin.New()
	router.Use(gin.Recovery(), middleware.RequestLogging(), middleware.CORS(cfg.CORSAllowedOrigins))

	healthHandler := &api.HealthHandler{DB: dbPool, Redis: redisClient, InstanceID: selfInstanceID}
	router.GET("/healthz", healthHandler.Healthz)
	router.GET("/readyz", healthHandler.Readyz)
	router.GET("/metrics", api.MetricsHandler)
	router.GET("/metrics.json", api.MetricsJSONHandler)

	// ===== Task2：用户体系与鉴权 =====
	tokenSvc := service.NewTokenService(cfg.JWTSecret, cfg.JWTExpiry)
	userRepo := repository.NewUserRepository(dbPool)
	authSvc := service.NewAuthService(userRepo, tokenSvc, cfg.AllowGuest)
	// 能力补齐项：登出后 token 立即失效（此前"登出"只是前端清 localStorage，
	// 服务端在 token 自然过期前依然认可其合法性）。HTTP 鉴权中间件与 WS 握手
	// 鉴权共用同一份黑名单实现，保证两条鉴权路径行为一致。
	tokenBlacklist := repository.NewRedisTokenBlacklist(redisClient)
	authSvc.SetTokenBlacklist(tokenBlacklist)
	authHandler := &api.AuthHandler{
		AuthService:         authSvc,
		SessionCookieSecure: cfg.SessionCookieSecure,
		OmitTokenResponse:   cfg.OmitTokenResponse,
	}
	// 命令重放保护：认证前按客户端 IP 隔离；认证后按 user_id 隔离。只有携带
	// Idempotency-Key 的调用启用该协议，不破坏已有客户端，同时为所有写接口提供
	// 可安全重试的标准入口。
	unauthIdempotency := middleware.Idempotency(redisClient, func(c *gin.Context) string { return "ip:" + c.ClientIP() })
	authIdempotency := middleware.Idempotency(redisClient, func(c *gin.Context) string { id, _ := middleware.UserIDFromContext(c); return "user:" + id })

	apiGroup := router.Group("/api")
	authGroup := apiGroup.Group("/auth")
	authGroup.POST("/register", unauthIdempotency, authHandler.Register)
	// 登录接口挂载暴力破解防护中间件（能力补齐项）：只对 /login 限流，不影响
	// /register、/guest——批量注册测试账号/访客模式是当前项目正常使用场景，
	// 限流的是"对同一账号高频猜密码"这一具体风险。
	authGroup.POST("/login", middleware.LoginRateLimiter(redisClient, cfg.LoginRateLimitPerMinute), unauthIdempotency, authHandler.Login)
	authGroup.POST("/guest", unauthIdempotency, authHandler.Guest)
	authGroup.GET("/session", middleware.RequireAuth(tokenSvc, tokenBlacklist), authHandler.Session)
	// 重放保护必须排在 logout 鉴权之前：首次登出已经把 token 拉黑时，同 key 的
	// 网络重试应命中首次 200 响应，而不是被后续鉴权误判为新的 401 请求。
	authGroup.POST("/logout", unauthIdempotency, middleware.RequireAuth(tokenSvc, tokenBlacklist), authHandler.Logout)

	// ===== Task3：兴趣聊天室管理 =====
	roomRepo := repository.NewRoomRepository(dbPool)
	messageRepo := repository.NewMessageRepository(dbPool)
	onlineCounter := repository.NewRedisRoomOnlineCounter(redisClient)
	roomSvc := service.NewRoomService(roomRepo, onlineCounter)
	messageSvc := service.NewMessageService(roomRepo, messageRepo)
	roomHandler := &api.RoomHandler{RoomService: roomSvc, MessageService: messageSvc}

	apiGroup.GET("/rooms", roomHandler.ListRooms) // 无需鉴权（T20：房间列表公开）
	apiGroup.GET("/rooms/:id/messages", roomHandler.ListRoomMessages)

	// 已鉴权路由分组：后续 Task（好友/私聊/房间管理等）挂载到这里，
	// 复用同一个 middleware.RequireAuth(tokenSvc, tokenBlacklist)。
	authedGroup := apiGroup.Group("/")
	authedGroup.Use(middleware.RequireAuth(tokenSvc, tokenBlacklist))
	authedGroup.POST("/rooms", authIdempotency, roomHandler.Create)

	// ===== Task14：好友关系链（先于 Hub 构造，Hub 需要以 ConversationService 作为
	// dmSender，而好友请求推送 FriendNotifier 又需要以 Hub 作为实现，
	// 因此 wsHub 必须在 friendSvc 之前、conversationSvc 之后构造，见下方顺序）=====
	friendRepo := repository.NewFriendshipRepository(dbPool)

	// ===== Task15：私聊（先于 Hub 构造） =====
	conversationRepo := repository.NewConversationRepository(dbPool)
	directMessageRepo := repository.NewDirectMessageRepository(dbPool)
	conversationSvc := service.NewConversationService(conversationRepo, directMessageRepo, friendRepo)

	// ===== Task17：Web Push（先于 Hub/FriendService 构造：二者都以 PushService 作为
	// 可选的 PushNotifier 依赖，通过 SetPushNotifier 注入，不改变已有构造函数签名） =====
	// presenceTracker 同时供 Hub（连接建立/断开时维护在线态）、Task14 好友在线态查询、
	// 与本处 PushService（判断是否需要发送离线通知）三处使用。
	presenceTracker := repository.NewRedisUserPresence(redisClient)
	pushSubRepo := repository.NewPushSubscriptionRepository(dbPool)
	pushSvc := service.NewPushService(pushSubRepo, presenceTracker, cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject)

	// ===== Task4：实时通信 WS Gateway =====
	// ★3 已确认：Redis Pub/Sub 多实例扇出为强制设计（非可选），见 hub.go 顶部注释。
	// 消息落库+幂等去重（T33/T34）在此一并接入（复用 Task3 的 messageRepo，见范围调整说明）。
	// rateLimitPerMinute 供 Hub 内 T40 限流使用（群聊+私聊共用同一连接级计数器）；
	// sensitiveWords 供 Hub 内 T81 敏感词过滤使用。
	wsHub := ws.NewHub(redisClient, messageRepo, cfg.MaxMessageLength, presenceTracker, conversationSvc, cfg.RateLimitPerMinute, cfg.SensitiveWords, selfInstanceID)
	wsHub.SetPushNotifier(pushSvc) // Task17/T103：私聊消息目标离线时触发 Web Push
	// 能力补齐项：给"未来投喂给模型训练用户替身"补最基础的行为原始数据，
	// join_room/send_message 两类事件在此接入（详见 model.InteractionEvent、
	// docs/24-training-data-pipeline-work-log.md）。
	interactionEventRepo := repository.NewInteractionEventRepository(dbPool)
	wsHub.SetEventRecorder(interactionEventRepo)
	// 复用登出黑名单（tokenBlacklist）；userRepo 同时实现了 ws.BotChecker
	// （能力补齐项：LLM 驱动机器人最小验证，握手时查 is_bot 决定 sender_type）。
	wsHandler := ws.NewHandlerWithOrigins(wsHub, tokenSvc, tokenBlacklist, userRepo, cfg.CORSAllowedOrigins)
	router.GET("/ws", gin.WrapF(wsHandler.ServeWS))

	// ===== Task14：好友关系链（接线路由） =====
	friendSvc := service.NewFriendService(friendRepo, userRepo, presenceTracker, wsHub)
	friendSvc.SetPushNotifier(pushSvc)               // Task17/T102：好友请求目标离线时触发 Web Push
	friendSvc.SetEventRecorder(interactionEventRepo) // 能力补齐项：add_friend_request 行为事件
	friendHandler := &api.FriendHandler{FriendService: friendSvc}

	authedGroup.POST("/friends/requests", authIdempotency, friendHandler.SendRequest)
	authedGroup.PUT("/friends/requests/:id", authIdempotency, friendHandler.RespondRequest)
	authedGroup.GET("/friends/requests", friendHandler.ListPendingRequests) // T120，本轮新增
	authedGroup.GET("/friends", friendHandler.ListFriends)
	authedGroup.DELETE("/friends/:id", authIdempotency, friendHandler.DeleteFriend)

	// ===== 本轮新增：用户查找/批量查询（补齐"添加好友按用户名查找""展示真实用户名"
	// 两处此前缺失的能力，见 T121/T122） =====
	userSvc := service.NewUserService(userRepo)
	userHandler := &api.UserHandler{UserService: userSvc}
	authedGroup.GET("/users/lookup", userHandler.Lookup)
	authedGroup.GET("/users", userHandler.BatchGet)
	authedGroup.GET("/me/profile", userHandler.GetMyProfile)
	authedGroup.PUT("/me/profile", authIdempotency, userHandler.UpdateMyProfile)

	// ===== Task17：Web Push（接线路由） =====
	pushHandler := &api.PushHandler{PushService: pushSvc}
	apiGroup.GET("/push/vapid-public-key", pushHandler.VAPIDPublicKey) // 无需鉴权
	authedGroup.POST("/push/subscriptions", authIdempotency, pushHandler.Subscribe)
	authedGroup.DELETE("/push/subscriptions", authIdempotency, pushHandler.Unsubscribe)

	// ===== Task15：私聊（接线路由；消息发送走 WS send_direct_message，见 ws/hub.go） =====
	conversationHandler := &api.ConversationHandler{ConversationService: conversationSvc}
	authedGroup.GET("/conversations", conversationHandler.ListConversations)
	authedGroup.GET("/conversations/:id/messages", conversationHandler.ListMessages)

	// ===== Task16：多媒体消息（P0 图片） =====
	// 本地磁盘存储 + gin.Static 提供访问（demo/评估项目简化，生产环境应替换为真实对象存储，
	// 已在文档中如实标注为简化点）。
	router.Static("/uploads", cfg.MediaUploadDir)
	mediaAssetRepo := repository.NewMediaAssetRepository(dbPool)
	mediaSvc := service.NewMediaService(mediaAssetRepo, cfg.MediaUploadDir, cfg.MaxUploadSizeBytes)
	mediaHandler := &api.MediaHandler{MediaService: mediaSvc}
	authedGroup.POST("/media/upload", authIdempotency, mediaHandler.Upload)

	// ===== Task18：内容安全（举报；敏感词过滤已接入 ws/hub.go） =====
	reportRepo := repository.NewReportRepository(dbPool)
	reportSvc := service.NewReportService(reportRepo, messageRepo, directMessageRepo, userRepo)
	reportHandler := &api.ReportHandler{ReportService: reportSvc}
	authedGroup.POST("/reports", authIdempotency, reportHandler.CreateReport)

	// ===== Task19：关注事项（P1，Task20 AI 推荐规则化匹配演示的输入源） =====
	watchTopicRepo := repository.NewWatchTopicRepository(dbPool)
	watchTopicSvc := service.NewWatchTopicService(watchTopicRepo)
	watchTopicHandler := &api.WatchTopicHandler{WatchTopicService: watchTopicSvc}
	authedGroup.POST("/watch-topics", authIdempotency, watchTopicHandler.Create)
	authedGroup.GET("/watch-topics", watchTopicHandler.List)
	authedGroup.DELETE("/watch-topics/:id", authIdempotency, watchTopicHandler.Delete) // T123，本轮新增

	// ===== Task20：AI 推荐规则化匹配演示（依赖 Task19 关注事项数据 + Task14 好友关系） =====
	matchCandidateRepo := repository.NewMatchCandidateRepository(dbPool)
	recommendationSvc := service.NewRecommendationService(matchCandidateRepo, watchTopicRepo, friendRepo, userRepo)
	recommendationHandler := &api.RecommendationHandler{RecommendationService: recommendationSvc}
	authedGroup.POST("/recommendations/generate", authIdempotency, recommendationHandler.Generate)
	authedGroup.GET("/recommendations", recommendationHandler.List)
	authedGroup.PUT("/recommendations/:id", authIdempotency, recommendationHandler.Respond)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
	}

	go func() {
		log.Info("http server starting", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	// 优雅关闭：收到 SIGTERM/SIGINT 后，等待正在处理的请求完成再退出（对应 Task6/T41）。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutdown signal received, starting graceful shutdown")

	// T41：先向所有活跃 WS 连接发送 Close 帧（协议层明确通知，而非被强制断连），
	// 再等待 HTTP 请求正常完成——WS 连接是 hijacked 连接，srv.Shutdown 不会等待它们，
	// 因此必须显式处理，不能假设 http.Server 的优雅关闭天然覆盖 WS。
	wsHub.Shutdown()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("graceful shutdown completed")
}
