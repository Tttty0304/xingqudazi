package service

import (
	"context"
	"errors"
	"fmt"

	"xingqudazi-im/server/internal/model"
)

// MessageStore 是消息历史查询所需的最小数据访问接口（写入路径在 Task5 接入
// send_message 时补充；本 Task3 只接入读路径，因为 GET /api/rooms/{id}/messages
// 与"是否已能发消息"解耦，可独立验证分页/排序逻辑）。
type MessageStore interface {
	// ListByRoom 按房间查询消息历史，游标为 createdBefore（用于分页，nil 表示取最新一页）。
	// 返回按时间倒序的消息列表，以及是否还有更早的消息（has_more）。
	ListByRoom(ctx context.Context, roomID string, page, size int) (messages []model.Message, hasMore bool, err error)
}

type MessageService struct {
	roomStore RoomStore
	msgStore  MessageStore
}

func NewMessageService(roomStore RoomStore, msgStore MessageStore) *MessageService {
	return &MessageService{roomStore: roomStore, msgStore: msgStore}
}

// ListRoomMessages 对应 Testcase T21/T22：先确认房间存在，再分页查询历史消息。
func (s *MessageService) ListRoomMessages(ctx context.Context, roomID string, page, size int) ([]model.Message, bool, error) {
	if _, err := s.roomStore.FindByID(ctx, roomID); err != nil {
		if errors.Is(err, ErrRepositoryRoomNotFound) {
			return nil, false, ErrRoomNotFound
		}
		return nil, false, fmt.Errorf("find room: %w", err)
	}

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	messages, hasMore, err := s.msgStore.ListByRoom(ctx, roomID, page, size)
	if err != nil {
		return nil, false, fmt.Errorf("list messages: %w", err)
	}
	return messages, hasMore, nil
}
