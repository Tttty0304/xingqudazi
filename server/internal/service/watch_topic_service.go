package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"xingqudazi-im/server/internal/model"
)

// WatchTopicStore 是 WatchTopicService 依赖的最小数据访问接口，真实实现见
// repository.WatchTopicRepository。
type WatchTopicStore interface {
	Create(ctx context.Context, t *model.WatchTopic) error
	ListByUser(ctx context.Context, userID string) ([]model.WatchTopic, error)
	// Delete 对应 T123（本轮新增）：删除仅限本人的关注事项，userID 用于越权校验，
	// 返回 deleted=false 表示未找到或非本人所有（不区分，与前端无需区分展示一致）。
	Delete(ctx context.Context, id, userID string) (deleted bool, err error)
}

// WatchTopicService 对应 Task19/P1：关注事项（用户主动声明"最近关注XX话题"），
// 是 Task20 AI 推荐规则化匹配演示的直接输入源，本次仅做增/查两个最小接口。
type WatchTopicService struct {
	store WatchTopicStore
}

func NewWatchTopicService(store WatchTopicStore) *WatchTopicService {
	return &WatchTopicService{store: store}
}

// CreateWatchTopic 对应 T94：创建关注事项。roomID 可为空字符串（全局关注）；
// expiresAt 为 nil 表示不过期。
func (s *WatchTopicService) CreateWatchTopic(ctx context.Context, userID, roomID, keywords string, priority int, expiresAt *time.Time) (*model.WatchTopic, error) {
	if keywords == "" {
		return nil, ErrInvalidWatchTopic
	}

	topic := &model.WatchTopic{
		ID:        uuid.NewString(),
		UserID:    userID,
		RoomID:    roomID,
		Keywords:  keywords,
		Priority:  priority,
		ExpiresAt: expiresAt,
	}
	if err := s.store.Create(ctx, topic); err != nil {
		return nil, fmt.Errorf("create watch topic: %w", err)
	}
	return topic, nil
}

// ListWatchTopics 对应 T95：查询当前用户全部关注事项。
func (s *WatchTopicService) ListWatchTopics(ctx context.Context, userID string) ([]model.WatchTopic, error) {
	topics, err := s.store.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list watch topics: %w", err)
	}
	return topics, nil
}

// DeleteWatchTopic 对应 T123（本轮新增）：删除关注事项，补齐此前"只能创建不能删除"
// 导致列表无法清理的半成品问题。
func (s *WatchTopicService) DeleteWatchTopic(ctx context.Context, id, userID string) error {
	deleted, err := s.store.Delete(ctx, id, userID)
	if err != nil {
		return fmt.Errorf("delete watch topic: %w", err)
	}
	if !deleted {
		return ErrWatchTopicNotFound
	}
	return nil
}
