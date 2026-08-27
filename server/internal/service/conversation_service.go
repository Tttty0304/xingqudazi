package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"xingqudazi-im/server/internal/model"
)

// ConversationStore 是 ConversationService 依赖的会话数据访问接口，真实实现见
// repository.ConversationRepository。
type ConversationStore interface {
	// GetOrCreate 获取或创建两用户之间的私聊会话（方向无关，规范化存储由实现负责）。
	GetOrCreate(ctx context.Context, userA, userB string) (*model.Conversation, error)
	FindByID(ctx context.Context, id string) (*model.Conversation, error)
	ListByUser(ctx context.Context, userID string) ([]model.ConversationSummary, error)
}

// ConversationReadMarker 为可选扩展接口，和其他服务的可选依赖注入保持一致。
// 真实 PostgreSQL 实现会持久化读游标；纯内存旧测试替身不实现时仍可验证会话主流程。
type ConversationReadMarker interface {
	MarkRead(ctx context.Context, userID, conversationID string) error
}

// DirectMessageStore 是私聊消息的数据访问接口，真实实现见 repository.DirectMessageRepository。
type DirectMessageStore interface {
	Create(ctx context.Context, msg *model.DirectMessage) (inserted bool, err error)
	ListByConversation(ctx context.Context, conversationID string, page, size int) (messages []model.DirectMessage, hasMore bool, err error)
}

// FriendChecker 查询两用户是否为已接受的好友关系（真实实现见
// repository.FriendshipRepository.IsFriend），供 T72（仅好友可私聊）校验使用。
type FriendChecker interface {
	IsFriend(ctx context.Context, userA, userB string) (bool, error)
}

type ConversationService struct {
	convStore ConversationStore
	dmStore   DirectMessageStore
	friends   FriendChecker
}

func NewConversationService(convStore ConversationStore, dmStore DirectMessageStore, friends FriendChecker) *ConversationService {
	return &ConversationService{convStore: convStore, dmStore: dmStore, friends: friends}
}

// SendDirectMessage 对应 T70（私聊发送）+ T72（已确认口径：仅好友可私聊）+
// Task16（contentType 支持 image，图片消息 Content 为媒体 URL）。
// msgID 由调用方（ws.Hub，与群聊 send_message 逻辑一致）生成/传入，用于幂等去重。
// 返回的 inserted=false 表示该 msgID 在此会话下重复提交，调用方应跳过广播。
func (s *ConversationService) SendDirectMessage(ctx context.Context, senderID, targetID, msgID, content, contentType string) (conversationID string, inserted bool, err error) {
	if senderID == targetID {
		return "", false, ErrCannotMessageSelf
	}

	ok, err := s.friends.IsFriend(ctx, senderID, targetID)
	if err != nil {
		return "", false, fmt.Errorf("check friendship: %w", err)
	}
	if !ok {
		return "", false, ErrFriendRequiredForDirectMessage
	}

	conv, err := s.convStore.GetOrCreate(ctx, senderID, targetID)
	if err != nil {
		return "", false, fmt.Errorf("get or create conversation: %w", err)
	}

	if contentType == "" {
		contentType = "text"
	}
	inserted, err = s.dmStore.Create(ctx, &model.DirectMessage{
		ID:             uuid.NewString(),
		MsgID:          msgID,
		ConversationID: conv.ID,
		SenderID:       senderID,
		SenderType:     "human",
		Content:        content,
		ContentType:    contentType,
	})
	if err != nil {
		return "", false, fmt.Errorf("create direct message: %w", err)
	}
	return conv.ID, inserted, nil
}

// ListConversations 对应 T71：会话列表 + 最近一条消息摘要。
func (s *ConversationService) ListConversations(ctx context.Context, userID string) ([]model.ConversationSummary, error) {
	summaries, err := s.convStore.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	return summaries, nil
}

// ListMessages 对应 T70 历史消息查询：仅会话参与者可查看（越权返回 403，同 T51/T61 风格）。
func (s *ConversationService) ListMessages(ctx context.Context, conversationID, requesterID string, page, size int) ([]model.DirectMessage, bool, error) {
	conv, err := s.convStore.FindByID(ctx, conversationID)
	if err != nil {
		if errors.Is(err, ErrRepositoryConversationNotFound) {
			return nil, false, ErrConversationNotFound
		}
		return nil, false, fmt.Errorf("find conversation: %w", err)
	}

	if conv.UserAID != requesterID && conv.UserBID != requesterID {
		return nil, false, ErrForbiddenConversationAccess
	}

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	messages, hasMore, err := s.dmStore.ListByConversation(ctx, conversationID, page, size)
	if err != nil {
		return nil, false, fmt.Errorf("list direct messages: %w", err)
	}
	if marker, ok := s.convStore.(ConversationReadMarker); ok {
		if err := marker.MarkRead(ctx, requesterID, conversationID); err != nil {
			return nil, false, fmt.Errorf("mark direct messages read: %w", err)
		}
	}
	return messages, hasMore, nil
}
