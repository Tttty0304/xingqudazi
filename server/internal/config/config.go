package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"xingqudazi-im/server/pkg/webpush"
)

// Config 汇总服务运行所需的全部环境相关配置。
// 所有字段均从环境变量读取，未提供时回落到合理默认值，方便本地开发直接跑起来。
type Config struct {
	// AppEnv 为 development 或 production。production 下会拒绝明显不安全的默认配置。
	AppEnv   string
	HTTPAddr string // 监听地址，如 ":8080"

	PostgresDSN string // PostgreSQL 连接串

	// PostgresMaxConns/PostgresMinConns 连接池上下限（Task10 性能与成本落地复核前
	// 硬编码为 10/1，本次改为可配置，默认值不变；压测/扩容时可通过环境变量调整，
	// 无需改代码重新编译）。
	PostgresMaxConns int32
	PostgresMinConns int32

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	JWTSecret  string
	JWTExpiry  time.Duration
	AllowGuest bool // 是否允许访客模式（★2 已确认支持）
	// SessionCookieSecure 令浏览器会话 Cookie 仅通过 HTTPS 发送；生产环境必须启用。
	SessionCookieSecure bool
	// OmitTokenResponse 生产浏览器模式不再在 JSON 响应中返回 JWT，避免前端脚本持久化。
	OmitTokenResponse bool

	// CORSAllowedOrigins 跨域白名单（能力补齐项：此前 CORS 硬编码为 `*`）。
	// 默认值 `["*"]` 保持完全放开（demo/评估项目默认行为不变），生产环境应
	// 通过 `CORS_ALLOWED_ORIGINS`（逗号分隔）配置为真实域名列表。
	CORSAllowedOrigins []string

	// ShutdownTimeout 优雅关闭时，等待正在处理的请求/连接完成的最长时间。
	ShutdownTimeout time.Duration

	// RateLimitPerMinute 单用户每分钟允许发言的消息数上限（Task6 限流用）。
	RateLimitPerMinute int

	// LoginRateLimitPerMinute 单 IP 每分钟允许尝试登录的次数上限（能力补齐：登录接口
	// 暴力破解防护）。此前 /api/auth/login 只做用户名密码校验，没有任何频率限制，
	// 攻击者可以对同一账号无限次猜密码；WS 消息发送早在 Task6 就有限流，登录接口
	// 反而缺失，是真实的安全缺口。
	LoginRateLimitPerMinute int

	// MaxMessageLength 单条消息允许的最大字符数。
	MaxMessageLength int

	// MediaUploadDir 图片上传的本地磁盘存储目录（Task16/P0，demo/评估项目简化为本地
	// 磁盘存储，生产环境应替换为真实对象存储，已在文档中如实标注）。
	MediaUploadDir string
	// MaxUploadSizeBytes 单个媒体文件允许的最大字节数（Task16/T92）。
	MaxUploadSizeBytes int64

	// SensitiveWords 基础敏感词库（Task18/T81），命中即拦截，不落库不广播。
	// demo/评估场景用固定小词表，生产环境应替换为可配置/可运营的词库管理。
	SensitiveWords []string

	// VAPIDPublicKey/VAPIDPrivateKey 是 Task17 Web Push 使用的应用服务器身份密钥
	// （RFC8292）。未通过环境变量配置时，进程启动时自动生成一对临时密钥——demo/评估
	// 场景可接受，代价是每次重启后浏览器旧订阅的 VAPID 校验会失效，需要用户重新订阅；
	// 生产环境应固定配置为环境变量，已在文档中如实标注这一简化点。
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	// VAPIDSubject 是 RFC8292 要求的 sub claim（联系方式，供推送服务运营方反馈问题）。
	VAPIDSubject string

	// LLMProvider/LLMAPIKey/LLMBaseURL/LLMModel 用于驱动机器人行为的 LLM 接入
	// （能力补齐项：给此前只有 schema 预留、从未真正跑通的"LLM驱动机器人参与
	// 社交"设计补一个最小验证，见 server/cmd/bot、pkg/llm）。核心 server 进程
	// 本身不依赖 LLM——只有独立的 cmd/bot 验证工具会读取这几项配置，未配置
	// 时不影响主服务启动。
	LLMProvider string
	// LLMAPIKey 优先读取 LLM_API_KEY，未设置时回落到 DASHSCOPE_API_KEY
	// （通义千问/百炼平台约定的环境变量名，兼容直接沿用该平台习惯命名的场景）。
	LLMAPIKey  string
	LLMBaseURL string
	LLMModel   string
}

// Load 从环境变量加载配置；调用方应在进程启动最早阶段调用一次。
func Load() (*Config, error) {
	appEnv := strings.ToLower(getEnv("APP_ENV", "development"))
	production := appEnv == "production"
	cfg := &Config{
		AppEnv:                  appEnv,
		HTTPAddr:                getEnv("HTTP_ADDR", ":8080"),
		PostgresDSN:             getEnv("POSTGRES_DSN", "postgres://im_user:im_password@localhost:5432/xingqudazi_im?sslmode=disable"),
		PostgresMaxConns:        int32(getEnvInt("POSTGRES_MAX_CONNS", 10)),
		PostgresMinConns:        int32(getEnvInt("POSTGRES_MIN_CONNS", 1)),
		RedisAddr:               getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:           getEnv("REDIS_PASSWORD", ""),
		RedisDB:                 getEnvInt("REDIS_DB", 0),
		JWTSecret:               getEnv("JWT_SECRET", "dev-only-secret-change-me"),
		JWTExpiry:               getEnvDuration("JWT_EXPIRY", 24*time.Hour),
		AllowGuest:              getEnvBool("ALLOW_GUEST", true),
		SessionCookieSecure:     getEnvBool("SESSION_COOKIE_SECURE", production),
		OmitTokenResponse:       getEnvBool("AUTH_OMIT_TOKEN_RESPONSE", production),
		CORSAllowedOrigins:      getEnvStringSlice("CORS_ALLOWED_ORIGINS", []string{"*"}),
		ShutdownTimeout:         getEnvDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
		RateLimitPerMinute:      getEnvInt("RATE_LIMIT_PER_MINUTE", 60),
		LoginRateLimitPerMinute: getEnvInt("LOGIN_RATE_LIMIT_PER_MINUTE", 10),
		MaxMessageLength:        getEnvInt("MAX_MESSAGE_LENGTH", 1000),
		MediaUploadDir:          getEnv("MEDIA_UPLOAD_DIR", "./uploads"),
		MaxUploadSizeBytes:      int64(getEnvInt("MAX_UPLOAD_SIZE_BYTES", 5*1024*1024)), // 默认 5MB
		SensitiveWords:          getEnvStringSlice("SENSITIVE_WORDS", []string{"badword1", "badword2", "违禁词示例"}),
		VAPIDPublicKey:          getEnv("VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateKey:         getEnv("VAPID_PRIVATE_KEY", ""),
		VAPIDSubject:            getEnv("VAPID_SUBJECT", "mailto:admin@example.com"),
		LLMProvider:             getEnv("LLM_PROVIDER", "qwen"),
		LLMAPIKey:               getEnvFirst("", "LLM_API_KEY", "DASHSCOPE_API_KEY"),
		LLMBaseURL:              getEnv("LLM_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
		LLMModel:                getEnv("LLM_MODEL", "qwen-plus"),
	}

	if cfg.VAPIDPublicKey == "" || cfg.VAPIDPrivateKey == "" {
		keys, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			return nil, fmt.Errorf("generate fallback vapid keys: %w", err)
		}
		cfg.VAPIDPublicKey = keys.PublicKey
		cfg.VAPIDPrivateKey = keys.PrivateKey
		fmt.Fprintln(os.Stderr, "[WARN] VAPID_PUBLIC_KEY/VAPID_PRIVATE_KEY 未设置，已自动生成临时密钥；"+
			"重启进程后旧的浏览器 Web Push 订阅将失效，生产环境应固定配置")
	}

	if cfg.JWTSecret == "dev-only-secret-change-me" {
		// 不阻断启动（demo/本地场景常见），但必须在日志中显著提示，避免误上生产。
		fmt.Fprintln(os.Stderr, "[WARN] JWT_SECRET 未设置，正在使用不安全的默认值，仅限本地开发")
	}
	if production {
		if len(cfg.JWTSecret) < 32 || cfg.JWTSecret == "dev-only-secret-change-me" {
			return nil, fmt.Errorf("production requires JWT_SECRET with at least 32 characters")
		}
		if !cfg.SessionCookieSecure {
			return nil, fmt.Errorf("production requires SESSION_COOKIE_SECURE=true")
		}
		for _, origin := range cfg.CORSAllowedOrigins {
			if origin == "*" {
				return nil, fmt.Errorf("production forbids CORS_ALLOWED_ORIGINS=*")
			}
		}
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvFirst 按顺序依次尝试多个环境变量名，返回第一个非空值；全部未设置时
// 回落到 fallback。用于兼容"同一份配置有多个约定名称"的场景（如
// LLMAPIKey 同时兼容通用的 LLM_API_KEY 与通义千问/百炼平台习惯使用的
// DASHSCOPE_API_KEY），避免用户必须严格记住某一个固定的变量名。
func getEnvFirst(fallback string, keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

// getEnvStringSlice 按逗号分隔解析环境变量为字符串切片，未设置时回落到默认值
// （Task18：SENSITIVE_WORDS 支持运营侧通过环境变量调整词库，不需要改代码重新编译）。
func getEnvStringSlice(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parts := strings.Split(v, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return fallback
	}
	return result
}
