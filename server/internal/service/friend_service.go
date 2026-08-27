package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"xingqudazi-im/server/internal/model"
)

// FriendshipStore 是 FriendService 依赖的最小数据访问接口，真实实现见
// repository.FriendshipRepository。
type FriendshipStore interface {
	Create(ctx context.Context, f *model.Friendship) (inserted bool, err error)
	FindByID(ctx context.Context, id string) (*model.Friendship, error)
	FindBetween(ctx context.Context, userA, userB string) (*model.Friendship, error)
	UpdateStatus(ctx context.Context, id, newStatus string) (updated bool, err error)
	ListAcceptedByUser(ctx context.Context, userID string) ([]model.Friendship, error)
	Delete(ctx context.Context, operatorID, peerID string) (deleted bool, err error)
	// ListPendingByUser 对应 T120（本轮新增）：查询当前用户涉及的全部待处理好友请求。
	ListPendingByUser(ctx context.Context, userID string) ([]model.Friendship, error)
}

// PresenceChecker 查询用户是否在线（真实实现见 repository.RedisUserPresence），
// 供 ListFriends 计算 T63 要求的实时 online 字段。
type PresenceChecker interface {
	IsOnline(ctx context.Context, userID string) (bool, error)
}

// FriendNotifier 用于向指定用户推送 WS 通知（T60：对方收到 friend_request_received）。
// 真实实现由 ws.Hub 提供，方法签名仅使用内建类型，避免 service 包反向依赖 ws 包。
type FriendNotifier interface {
	NotifyFriendRequestReceived(ctx context.Context, targetUserID, requestID, fromUserID string) error
}

// PushNotifier 用于在目标用户离线时发送 Web Push 通知（Task17/T102），真实实现见
// service.PushService.NotifyOfflineUser——之所以在 service 包内部再定义一个同构接口
// （而不是直接依赖 *PushService 具体类型），是为了让 FriendService 的单测可以注入
// 一个不触发真实网络请求的假实现，与 FriendNotifier/PresenceChecker 保持同款可测试性设计。
type PushNotifier interface {
	NotifyOfflineUser(ctx context.Context, userID, title, body string) error
}

// EventRecorder 供 FriendService 在发生好友请求行为时记录一条
// interaction_events 行为事件（能力补齐项：给"未来投喂给模型训练用户替身"
// 补最基础的行为原始数据），真实实现见
// repository.InteractionEventRepository。可为 nil：未注入时静默跳过，与
// PushNotifier 的可选注入原则一致。
type EventRecorder interface {
	Create(ctx context.Context, e *model.InteractionEvent) error
}

type FriendService struct {
	store         FriendshipStore
	users         UserStore
	presence      PresenceChecker
	notifier      FriendNotifier
	pusher        PushNotifier  // 可为 nil：未配置 Web Push 时静默跳过，不影响好友请求主流程
	eventRecorder EventRecorder // 可为 nil：未注入时静默跳过，不影响任何现有功能
}

func NewFriendService(store FriendshipStore, users UserStore, presence PresenceChecker, notifier FriendNotifier) *FriendService {
	return &FriendService{store: store, users: users, presence: presence, notifier: notifier}
}

// SetPushNotifier 注入 Task17 Web Push 能力（可选，向后兼容旧构造方式，不改变
// NewFriendService 签名，避免影响既有调用点/测试）。
func (s *FriendService) SetPushNotifier(p PushNotifier) {
	s.pusher = p
}

// SetEventRecorder 注入行为事件记录能力（能力补齐项，可选，同款不改变
// NewFriendService 签名的注入方式）。
func (s *FriendService) SetEventRecorder(r EventRecorder) {
	s.eventRecorder = r
}

// SendRequest 对应 T60：发起好友请求。
func (s *FriendService) SendRequest(ctx context.Context, requesterID, targetID string) (*model.Friendship, error) {
	if requesterID == targetID {
		return nil, ErrCannotFriendSelf
	}

	if _, err := s.users.FindByID(ctx, targetID); err != nil {
		if errors.Is(err, ErrRepositoryUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("find target user: %w", err)
	}

	existing, err := s.store.FindBetween(ctx, requesterID, targetID)
	if err != nil && !errors.Is(err, ErrRepositoryFriendshipNotFound) {
		return nil, fmt.Errorf("find existing friendship: %w", err)
	}
	if existing != nil {
		if existing.Status == "accepted" {
			return nil, ErrAlreadyFriends
		}
		// pending（无论哪一方发起）或 rejected 后再次发起，均视为"已有请求记录"，
		// 不重复创建（T60 边界：重复发起同一好友请求幂等/拒绝）。
		return nil, ErrFriendRequestExists
	}

	friendship := &model.Friendship{
		ID:          uuid.NewString(),
		RequesterID: requesterID,
		TargetID:    targetID,
		Status:      "pending",
	}
	inserted, err := s.store.Create(ctx, friendship)
	if err != nil {
		return nil, fmt.Errorf("create friendship: %w", err)
	}
	if !inserted {
		// 并发竞态下唯一约束兜底命中，语义等价于"已存在请求"。
		return nil, ErrFriendRequestExists
	}

	// 能力补齐项：记录行为事件，只在真正创建成功（走到这里）之后才记，
	// 失败/已存在/竞态命中等分支均不记，避免训练数据里混入噪音。记录失败
	// 不阻塞好友请求已创建成功的事实（与下方 pusher 调用同款"最佳努力"处理，
	// service 包不持有 logger，遵循既有的 `_ =` 静默处理风格）。
	if s.eventRecorder != nil {
		_ = s.eventRecorder.Create(ctx, &model.InteractionEvent{
			ID:           uuid.NewString(),
			UserID:       requesterID,
			TargetUserID: &targetID,
			EventType:    model.EventTypeAddFriendRequest,
		})
	}

	if s.notifier != nil {
		if err := s.notifier.NotifyFriendRequestReceived(ctx, targetID, friendship.ID, requesterID); err != nil {
			// WS 推送失败不影响请求已创建的事实（对方离线时本就不会收到，属预期），仅记录，
			// 由调用方（api 层）决定是否记日志；这里不吞掉错误，直接返回给上层判断。
			return friendship, fmt.Errorf("notify friend request received: %w", err)
		}
	}

	// Task17/T102：对方离线时额外发一条 Web Push（在线用户已通过上面的 WS 通知实时收到，
	// 这里的 NotifyOfflineUser 内部会自行判断在线态，在线时静默跳过）。Push 发送失败
	// （如全部订阅已失效）不应影响好友请求已创建成功的事实，仅记录，不向上抛出阻断请求。
	if s.pusher != nil {
		requester, err := s.users.FindByID(ctx, requesterID)
		if err == nil {
			_ = s.pusher.NotifyOfflineUser(ctx, targetID, "新的好友请求", requester.Username+" 想加你为好友")
		}
	}

	return friendship, nil
}

// friendRequestAction 校验并规范化 T61 的 action 字段。
func friendRequestAction(action string) (status string, ok bool) {
	switch action {
	case "accept":
		return "accepted", true
	case "reject":
		return "rejected", true
	default:
		return "", false
	}
}

// RespondRequest 对应 T61/T62：接受或拒绝好友请求。actorID 必须是请求的接收方。
func (s *FriendService) RespondRequest(ctx context.Context, requestID, actorID, action string) (*model.Friendship, error) {
	newStatus, ok := friendRequestAction(action)
	if !ok {
		return nil, ErrInvalidFriendAction
	}

	friendship, err := s.store.FindByID(ctx, requestID)
	if err != nil {
		if errors.Is(err, ErrRepositoryFriendshipNotFound) {
			return nil, ErrFriendRequestNotFound
		}
		return nil, fmt.Errorf("find friendship: %w", err)
	}

	// T51 同款越权校验：只有请求接收方才能操作。
	if friendship.TargetID != actorID {
		return nil, ErrForbiddenFriendResponse
	}

	if friendship.Status != "pending" {
		return nil, ErrFriendRequestResolved
	}

	updated, err := s.store.UpdateStatus(ctx, requestID, newStatus)
	if err != nil {
		return nil, fmt.Errorf("update friendship status: %w", err)
	}
	if !updated {
		// 并发下被其他请求先处理，语义等价于"已处理过"（T62）。
		return nil, ErrFriendRequestResolved
	}

	friendship.Status = newStatus
	return friendship, nil
}

// ListFriends 对应 T63：好友列表 + 实时在线态。
func (s *FriendService) ListFriends(ctx context.Context, userID string) ([]model.Friend, error) {
	friendships, err := s.store.ListAcceptedByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list friendships: %w", err)
	}

	result := make([]model.Friend, 0, len(friendships))
	for _, f := range friendships {
		// 好友关系不区分方向，取"不是当前用户"的那一方作为好友。
		peerID := f.TargetID
		if f.RequesterID != userID {
			peerID = f.RequesterID
		}

		peer, err := s.users.FindByID(ctx, peerID)
		if err != nil {
			// 对端用户异常（理论上不应发生，外键约束保证存在）不应导致整个列表接口失败。
			continue
		}

		online := false
		if s.presence != nil {
			if v, err := s.presence.IsOnline(ctx, peerID); err == nil {
				online = v
			}
			// 在线态查询失败时优雅降级为 false，与 RoomService.ListRooms 对非核心依赖异常的处理方式一致。
		}

		result = append(result, model.Friend{UserID: peer.ID, Username: peer.Username, Online: online})
	}
	return result, nil
}

// ListPendingRequests 对应 T120（本轮新增）：返回当前用户视角下全部待处理好友请求，
// 同时补全对方用户名，并按请求发起方标注方向（incoming：别人发给我，可接受/拒绝；
// outgoing：我发给别人，仅能查看等待状态）。这是好友请求 UI 能"离线后事后查看/处理"
// 的必要前提——此前只有 WS 实时通知，接收方离线时错过通知就再也看不到该请求。
func (s *FriendService) ListPendingRequests(ctx context.Context, userID string) ([]model.PendingFriendRequest, error) {
	friendships, err := s.store.ListPendingByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list pending friendships: %w", err)
	}

	result := make([]model.PendingFriendRequest, 0, len(friendships))
	for _, f := range friendships {
		direction := "incoming"
		peerID := f.RequesterID
		if f.RequesterID == userID {
			direction = "outgoing"
			peerID = f.TargetID
		}

		peer, err := s.users.FindByID(ctx, peerID)
		if err != nil {
			// 对端用户异常理论上不应发生（外键约束保证存在），不应导致整个列表接口失败。
			continue
		}

		result = append(result, model.PendingFriendRequest{
			RequestID:    f.ID,
			PeerID:       peer.ID,
			PeerUsername: peer.Username,
			Direction:    direction,
			CreatedAt:    f.CreatedAt,
		})
	}
	return result, nil
}

// RemoveFriend 对应 Plan Part3 `DELETE /api/friends/{id}`（id 为对方 user_id）。
func (s *FriendService) RemoveFriend(ctx context.Context, operatorID, peerID string) error {
	deleted, err := s.store.Delete(ctx, operatorID, peerID)
	if err != nil {
		return fmt.Errorf("delete friendship: %w", err)
	}
	if !deleted {
		return ErrNotFriends
	}
	return nil
}
