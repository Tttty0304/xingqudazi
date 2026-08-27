package config

import (
	"os"
	"testing"
	"time"
)

// clearEnv 在测试前后清理指定环境变量，避免测试之间/与本机真实环境变量互相污染
// （能力补齐项：此前 internal/config 覆盖率为 0%，环境变量解析的 fallback/覆盖
// 逻辑完全没有单测直接验证）。
func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	originals := make(map[string]string, len(keys))
	existed := make(map[string]bool, len(keys))
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			originals[k] = v
			existed[k] = true
		}
		os.Unsetenv(k)
	}
	t.Cleanup(func() {
		for _, k := range keys {
			if existed[k] {
				os.Setenv(k, originals[k])
			} else {
				os.Unsetenv(k)
			}
		}
	})
}

func TestGetEnv_FallbackWhenUnset(t *testing.T) {
	clearEnv(t, "TEST_STR_KEY")
	if got := getEnv("TEST_STR_KEY", "default-value"); got != "default-value" {
		t.Fatalf("expected fallback, got %q", got)
	}
}

func TestGetEnv_UsesSetValue(t *testing.T) {
	clearEnv(t, "TEST_STR_KEY")
	os.Setenv("TEST_STR_KEY", "custom-value")
	if got := getEnv("TEST_STR_KEY", "default-value"); got != "custom-value" {
		t.Fatalf("expected custom-value, got %q", got)
	}
}

func TestGetEnvInt_FallbackWhenUnsetOrInvalid(t *testing.T) {
	clearEnv(t, "TEST_INT_KEY")
	if got := getEnvInt("TEST_INT_KEY", 42); got != 42 {
		t.Fatalf("expected fallback 42, got %d", got)
	}

	os.Setenv("TEST_INT_KEY", "not-a-number")
	if got := getEnvInt("TEST_INT_KEY", 42); got != 42 {
		t.Fatalf("expected fallback 42 for invalid int, got %d", got)
	}

	os.Setenv("TEST_INT_KEY", "100")
	if got := getEnvInt("TEST_INT_KEY", 42); got != 100 {
		t.Fatalf("expected 100, got %d", got)
	}
}

func TestGetEnvBool_FallbackWhenUnsetOrInvalid(t *testing.T) {
	clearEnv(t, "TEST_BOOL_KEY")
	if got := getEnvBool("TEST_BOOL_KEY", true); !got {
		t.Fatal("expected fallback true")
	}

	os.Setenv("TEST_BOOL_KEY", "not-a-bool")
	if got := getEnvBool("TEST_BOOL_KEY", true); !got {
		t.Fatal("expected fallback true for invalid bool")
	}

	os.Setenv("TEST_BOOL_KEY", "false")
	if got := getEnvBool("TEST_BOOL_KEY", true); got {
		t.Fatal("expected false from explicit env value")
	}
}

func TestGetEnvDuration_FallbackWhenUnsetOrInvalid(t *testing.T) {
	clearEnv(t, "TEST_DURATION_KEY")
	if got := getEnvDuration("TEST_DURATION_KEY", 5*time.Second); got != 5*time.Second {
		t.Fatalf("expected fallback 5s, got %v", got)
	}

	os.Setenv("TEST_DURATION_KEY", "not-a-duration")
	if got := getEnvDuration("TEST_DURATION_KEY", 5*time.Second); got != 5*time.Second {
		t.Fatalf("expected fallback 5s for invalid duration, got %v", got)
	}

	os.Setenv("TEST_DURATION_KEY", "30s")
	if got := getEnvDuration("TEST_DURATION_KEY", 5*time.Second); got != 30*time.Second {
		t.Fatalf("expected 30s, got %v", got)
	}
}

func TestGetEnvStringSlice_FallbackWhenUnset(t *testing.T) {
	clearEnv(t, "TEST_SLICE_KEY")
	fallback := []string{"a", "b"}
	got := getEnvStringSlice("TEST_SLICE_KEY", fallback)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("expected fallback slice, got %v", got)
	}
}

// TestGetEnvStringSlice_ParsesCommaSeparated 对应能力补齐项：SENSITIVE_WORDS/
// CORS_ALLOWED_ORIGINS 均按逗号分隔解析，且自动裁剪空白与空字符串项。
func TestGetEnvStringSlice_ParsesCommaSeparated(t *testing.T) {
	clearEnv(t, "TEST_SLICE_KEY")
	os.Setenv("TEST_SLICE_KEY", "foo, bar ,,baz")
	got := getEnvStringSlice("TEST_SLICE_KEY", []string{"fallback"})
	want := []string{"foo", "bar", "baz"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

// TestGetEnvStringSlice_AllEmptyPartsFallsBack 覆盖边界：环境变量设置了但内容
// 全是空白/逗号（解析结果为空切片）时应回退到默认值，而不是返回一个空列表
// （空敏感词库/空 CORS 白名单在业务语义上很危险，不应该被环境变量的意外空值触发）。
func TestGetEnvStringSlice_AllEmptyPartsFallsBack(t *testing.T) {
	clearEnv(t, "TEST_SLICE_KEY")
	os.Setenv("TEST_SLICE_KEY", " , , ")
	fallback := []string{"safe-default"}
	got := getEnvStringSlice("TEST_SLICE_KEY", fallback)
	if len(got) != 1 || got[0] != "safe-default" {
		t.Fatalf("expected fallback when all parts are empty, got %v", got)
	}
}

// TestLoad_GeneratesVAPIDKeysWhenUnset 覆盖 Load() 在未配置 VAPID 密钥时自动生成
// 临时密钥对的兜底逻辑（不应因为缺少这两个环境变量而启动失败）。
func TestLoad_GeneratesVAPIDKeysWhenUnset(t *testing.T) {
	clearEnv(t, "VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY", "HTTP_ADDR", "JWT_SECRET")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() should not fail when VAPID keys are unset, got: %v", err)
	}
	if cfg.VAPIDPublicKey == "" || cfg.VAPIDPrivateKey == "" {
		t.Fatal("expected auto-generated VAPID keys to be non-empty")
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("expected default HTTPAddr :8080, got %q", cfg.HTTPAddr)
	}
}

// TestGetEnvFirst_TriesKeysInOrder 覆盖能力补齐项（LLM 驱动机器人最小验证）
// 新增的 getEnvFirst：LLM_API_KEY/DASHSCOPE_API_KEY 两个约定名称，优先取第一个
// 已设置的非空值，全部未设置时回落到 fallback。
func TestGetEnvFirst_TriesKeysInOrder(t *testing.T) {
	clearEnv(t, "TEST_FIRST_A", "TEST_FIRST_B")
	if got := getEnvFirst("fallback", "TEST_FIRST_A", "TEST_FIRST_B"); got != "fallback" {
		t.Fatalf("expected fallback when both unset, got %q", got)
	}

	os.Setenv("TEST_FIRST_B", "value-from-b")
	if got := getEnvFirst("fallback", "TEST_FIRST_A", "TEST_FIRST_B"); got != "value-from-b" {
		t.Fatalf("expected value-from-b when only second key is set, got %q", got)
	}

	os.Setenv("TEST_FIRST_A", "value-from-a")
	if got := getEnvFirst("fallback", "TEST_FIRST_A", "TEST_FIRST_B"); got != "value-from-a" {
		t.Fatalf("expected first key to take priority when both set, got %q", got)
	}
}

// TestLoad_LLMDefaults 覆盖能力补齐项新增的 LLM_* 配置：未设置时的默认值
// （LLM_PROVIDER=qwen、LLM_API_KEY 默认为空——核心 server 进程本身不依赖
// LLM，空值不应导致 Load() 失败）。
func TestLoad_LLMDefaults(t *testing.T) {
	clearEnv(t, "LLM_PROVIDER", "LLM_API_KEY", "DASHSCOPE_API_KEY", "LLM_BASE_URL", "LLM_MODEL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() should not fail when LLM_* env vars are unset, got: %v", err)
	}
	if cfg.LLMProvider != "qwen" {
		t.Errorf("expected default LLMProvider=qwen, got %q", cfg.LLMProvider)
	}
	if cfg.LLMAPIKey != "" {
		t.Errorf("expected empty LLMAPIKey by default, got %q", cfg.LLMAPIKey)
	}
	if cfg.LLMBaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Errorf("unexpected default LLMBaseURL: %q", cfg.LLMBaseURL)
	}
	if cfg.LLMModel != "qwen-plus" {
		t.Errorf("unexpected default LLMModel: %q", cfg.LLMModel)
	}
}

// TestLoad_LLMAPIKey_FallsBackToDashScopeKey 覆盖：只设置 DASHSCOPE_API_KEY
// 时（通义千问/百炼平台习惯做法）也能被正确读取。
func TestLoad_LLMAPIKey_FallsBackToDashScopeKey(t *testing.T) {
	clearEnv(t, "LLM_API_KEY", "DASHSCOPE_API_KEY")
	os.Setenv("DASHSCOPE_API_KEY", "sk-test-dashscope-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.LLMAPIKey != "sk-test-dashscope-key" {
		t.Fatalf("expected LLMAPIKey to fall back to DASHSCOPE_API_KEY, got %q", cfg.LLMAPIKey)
	}
}

// TestLoad_RespectsExplicitEnvOverrides 覆盖 Load() 正确读取显式设置的环境变量
// （而不是全部使用默认值），是配置系统最基本但也最容易被忽略验证的场景。
func TestLoad_RespectsExplicitEnvOverrides(t *testing.T) {
	clearEnv(t, "HTTP_ADDR", "ALLOW_GUEST", "RATE_LIMIT_PER_MINUTE", "LOGIN_RATE_LIMIT_PER_MINUTE",
		"CORS_ALLOWED_ORIGINS", "VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY")
	os.Setenv("HTTP_ADDR", ":9999")
	os.Setenv("ALLOW_GUEST", "false")
	os.Setenv("RATE_LIMIT_PER_MINUTE", "120")
	os.Setenv("LOGIN_RATE_LIMIT_PER_MINUTE", "5")
	os.Setenv("CORS_ALLOWED_ORIGINS", "https://a.example.com,https://b.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.HTTPAddr != ":9999" {
		t.Errorf("expected HTTPAddr=:9999, got %q", cfg.HTTPAddr)
	}
	if cfg.AllowGuest {
		t.Error("expected AllowGuest=false")
	}
	if cfg.RateLimitPerMinute != 120 {
		t.Errorf("expected RateLimitPerMinute=120, got %d", cfg.RateLimitPerMinute)
	}
	if cfg.LoginRateLimitPerMinute != 5 {
		t.Errorf("expected LoginRateLimitPerMinute=5, got %d", cfg.LoginRateLimitPerMinute)
	}
	if len(cfg.CORSAllowedOrigins) != 2 || cfg.CORSAllowedOrigins[0] != "https://a.example.com" {
		t.Errorf("expected 2 CORS origins parsed from env, got %v", cfg.CORSAllowedOrigins)
	}
}

func TestLoad_ProductionRejectsUnsafeDefaults(t *testing.T) {
	keys := []string{"APP_ENV", "JWT_SECRET", "SESSION_COOKIE_SECURE", "CORS_ALLOWED_ORIGINS", "VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY"}
	clearEnv(t, keys...)
	os.Setenv("APP_ENV", "production")
	os.Setenv("JWT_SECRET", "this-is-a-long-enough-production-secret-value")
	os.Setenv("SESSION_COOKIE_SECURE", "true")
	os.Setenv("CORS_ALLOWED_ORIGINS", "*")
	os.Setenv("VAPID_PUBLIC_KEY", "public")
	os.Setenv("VAPID_PRIVATE_KEY", "private")
	if _, err := Load(); err == nil {
		t.Fatal("expected production wildcard CORS to be rejected")
	}

	os.Setenv("CORS_ALLOWED_ORIGINS", "https://chat.example.com")
	os.Setenv("SESSION_COOKIE_SECURE", "false")
	if _, err := Load(); err == nil {
		t.Fatal("expected production insecure session cookie to be rejected")
	}

	os.Setenv("SESSION_COOKIE_SECURE", "true")
	os.Setenv("JWT_SECRET", "too-short")
	if _, err := Load(); err == nil {
		t.Fatal("expected short production JWT secret to be rejected")
	}
}

func TestLoad_ProductionAcceptsExplicitSafeSettings(t *testing.T) {
	keys := []string{"APP_ENV", "JWT_SECRET", "SESSION_COOKIE_SECURE", "CORS_ALLOWED_ORIGINS", "VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY"}
	clearEnv(t, keys...)
	os.Setenv("APP_ENV", "production")
	os.Setenv("JWT_SECRET", "this-is-a-long-enough-production-secret-value")
	os.Setenv("SESSION_COOKIE_SECURE", "true")
	os.Setenv("CORS_ALLOWED_ORIGINS", "https://chat.example.com")
	os.Setenv("VAPID_PUBLIC_KEY", "public")
	os.Setenv("VAPID_PRIVATE_KEY", "private")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected safe production configuration to load: %v", err)
	}
	if !cfg.OmitTokenResponse || !cfg.SessionCookieSecure {
		t.Fatalf("expected secure production defaults, got %+v", cfg)
	}
}
