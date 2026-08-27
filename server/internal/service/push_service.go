package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/pkg/webpush"
)

// PushSubscriptionStore 是 PushService 依赖的最小数据访问接口，真实实现见
// repository.PushSubscriptionRepository。
type PushSubscriptionStore interface {
	Create(ctx context.Context, s *model.PushSubscription) error
	DeleteByEndpoint(ctx context.Context, userID, endpoint string) error
	ListByUser(ctx context.Context, userID string) ([]model.PushSubscription, error)
	DeleteByID(ctx context.Context, id string) error
}

// pushPayload 是发往浏览器 Service Worker 的通知负载 JSON 结构（Task17）。
// 前端（Task9，尚未实现）在 Service Worker 的 `push` 事件里解析这段 JSON 后调用
// `Notification` API 展示系统通知——本次仅实现后端契约，不含前端 Service Worker 代码。
type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// PushService 实现 Task17（Web Push 离线通知）的订阅管理与发送触发逻辑。
// 触发时机：好友请求（FriendService）/私聊消息（ws.Hub）在目标用户当前**离线**
// （无活跃 WS 连接）时才发送 Web Push，在线用户已经能通过 WS 实时收到，无需重复通知
// ——这也是"离线通知"这个名字的字面含义，不是"每条消息都推送"。
type PushService struct {
	store    PushSubscriptionStore
	presence PresenceChecker
	vapid    *webpush.VAPIDKeys
	subject  string
}

func NewPushService(store PushSubscriptionStore, presence PresenceChecker, vapidPublicKey, vapidPrivateKey, subject string) *PushService {
	return &PushService{
		store:    store,
		presence: presence,
		vapid:    &webpush.VAPIDKeys{PublicKey: vapidPublicKey, PrivateKey: vapidPrivateKey},
		subject:  subject,
	}
}

// VAPIDPublicKey 供前端（Task9）在调用 `PushManager.subscribe()` 时作为
// `applicationServerKey` 传入；本次通过 `GET /api/push/vapid-public-key` 暴露。
func (s *PushService) VAPIDPublicKey() string {
	return s.vapid.PublicKey
}

// Subscribe 对应浏览器完成 `PushManager.subscribe()` 后把订阅信息上报给后端（T100）。
func (s *PushService) Subscribe(ctx context.Context, userID, endpoint, p256dh, auth string) error {
	if endpoint == "" || p256dh == "" || auth == "" {
		return ErrInvalidPushSubscription
	}
	sub := &model.PushSubscription{
		ID:       uuid.NewString(),
		UserID:   userID,
		Endpoint: endpoint,
		P256dh:   p256dh,
		Auth:     auth,
	}
	if err := s.store.Create(ctx, sub); err != nil {
		return fmt.Errorf("create push subscription: %w", err)
	}
	return nil
}

// Unsubscribe 对应浏览器 `PushManager.unsubscribe()` 后通知后端清理（T101）。
func (s *PushService) Unsubscribe(ctx context.Context, userID, endpoint string) error {
	if err := s.store.DeleteByEndpoint(ctx, userID, endpoint); err != nil {
		return fmt.Errorf("delete push subscription: %w", err)
	}
	return nil
}

// NotifyOfflineUser 是 ws.PushNotifier / FriendService 依赖的触发入口（T102/T103）：
// 若用户当前在线（有活跃 WS 连接），直接跳过（WS 已实时送达，无需重复通知）；
// 若离线，向其名下全部订阅端点发送 Web Push；单条订阅发送失败（如返回 404/410，
// 代表浏览器端订阅已失效）时清理该订阅，不影响其它订阅端点的发送。
func (s *PushService) NotifyOfflineUser(ctx context.Context, userID, title, body string) error {
	online, err := s.presence.IsOnline(ctx, userID)
	if err != nil {
		return fmt.Errorf("check presence: %w", err)
	}
	if online {
		return nil
	}

	subs, err := s.store.ListByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list push subscriptions: %w", err)
	}
	if len(subs) == 0 {
		return nil
	}

	payload, err := json.Marshal(pushPayload{Title: title, Body: body})
	if err != nil {
		return fmt.Errorf("marshal push payload: %w", err)
	}

	var lastErr error
	for _, sub := range subs {
		if err := s.sendWithRetry(ctx, sub, payload); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// pushSendMaxAttempts/pushSendRetryBackoff 是发送失败重试的参数（能力补齐项：
// 此前单条订阅发送失败就直接放弃，不做任何重试——考虑到 Web Push 端点是外部
// 浏览器厂商服务，瞬时网络抖动/短暂 5xx 是常见场景，值得用少量重试换取更高的
// 送达率。仅重试网络错误/5xx；4xx（含 404/410 订阅失效）语义明确，重试无意义，
// 不纳入重试范围）。
const pushSendMaxAttempts = 3

var pushSendRetryBackoff = [pushSendMaxAttempts]time.Duration{0, 200 * time.Millisecond, 500 * time.Millisecond}

// sendWithRetry 向单条订阅端点发送一次推送，网络错误/5xx 最多重试到
// pushSendMaxAttempts 次（含首次），4xx 直接判定为不可重试的最终结果。
func (s *PushService) sendWithRetry(ctx context.Context, sub model.PushSubscription, payload []byte) error {
	var lastErr error
	for attempt := 0; attempt < pushSendMaxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pushSendRetryBackoff[attempt]):
			}
		}

		resp, err := webpush.Send(ctx, webpush.SendOptions{
			Subscription: webpush.Subscription{Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth},
			Payload:      payload,
			VAPID:        s.vapid,
			Subject:      s.subject,
			TTL:          24 * time.Hour,
		})
		if err != nil {
			// 网络层错误（DNS/连接超时等），值得重试。
			lastErr = err
			continue
		}
		resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			// 订阅已失效（浏览器已取消订阅但未来得及/未能调用 Unsubscribe 通知
			// 后端），清理僵尸订阅；这是明确的最终状态，不重试。
			_ = s.store.DeleteByID(ctx, sub.ID)
			return nil
		case resp.StatusCode >= 500:
			// 推送服务瞬时故障，值得重试。
			lastErr = fmt.Errorf("push service returned %d", resp.StatusCode)
			continue
		case resp.StatusCode >= 400:
			// 其它 4xx（如 VAPID 校验失败/负载过大）语义明确，重试无意义。
			return fmt.Errorf("push service returned %d", resp.StatusCode)
		default:
			return nil
		}
	}
	return lastErr
}
