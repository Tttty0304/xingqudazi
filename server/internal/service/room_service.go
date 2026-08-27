package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"xingqudazi-im/server/internal/model"
)

// ErrRepositoryRoomNotFound 由 RoomStore 实现在查不到房间时返回。
var ErrRepositoryRoomNotFound = errors.New("repository_room_not_found")

// RoomStore 是 RoomService 依赖的最小数据访问接口。
type RoomStore interface {
	List(ctx context.Context) ([]model.Room, error)
	FindByID(ctx context.Context, id string) (*model.Room, error)
}

type RoomCreator interface {
	Create(ctx context.Context, room *model.Room) error
}

// OnlineCounter 提供房间在线人数查询（真实实现在 Task4 由 Redis 维护，
// 当前 Task3 阶段房间列表接口需要这个依赖但尚无真实在线连接，
// 因此单测可直接注入返回固定值的假实现，生产实现见 repository.RedisRoomOnlineCounter）。
type OnlineCounter interface {
	Count(ctx context.Context, roomID string) (int64, error)
}

type RoomService struct {
	store   RoomStore
	counter OnlineCounter
}

func NewRoomService(store RoomStore, counter OnlineCounter) *RoomService {
	return &RoomService{store: store, counter: counter}
}

// ListRooms 对应 Testcase T20：GET /api/rooms，无需鉴权，返回全部房间+实时在线人数。
func (s *RoomService) ListRooms(ctx context.Context) ([]model.RoomWithOnlineCount, error) {
	rooms, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}

	result := make([]model.RoomWithOnlineCount, 0, len(rooms))
	for _, r := range rooms {
		count, err := s.counter.Count(ctx, r.ID)
		if err != nil {
			// 在线人数查询失败不应导致整个房间列表接口失败（可靠性要求：
			// 一个非核心依赖异常时优雅降级，而不是让核心浏览功能一起挂掉）。
			count = 0
		}
		result = append(result, model.RoomWithOnlineCount{Room: r, OnlineCount: count})
	}
	return result, nil
}

// GetRoom 对应 Testcase T22：房间不存在返回 ErrRoomNotFound。
func (s *RoomService) GetRoom(ctx context.Context, roomID string) (*model.Room, error) {
	room, err := s.store.FindByID(ctx, roomID)
	if err != nil {
		if errors.Is(err, ErrRepositoryRoomNotFound) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("find room: %w", err)
	}
	return room, nil
}

// CreateRoom 允许已登录用户创建非预置兴趣房间。名称和主题在服务层统一校验，
// 保证 HTTP/未来内部调用不会绕过边界；持久化由命令幂等中间件保护。
func (s *RoomService) CreateRoom(ctx context.Context, creatorID, name, topic string) (*model.Room, error) {
	name = strings.TrimSpace(name)
	topic = strings.TrimSpace(topic)
	if utf8.RuneCountInString(name) < 2 || utf8.RuneCountInString(name) > 64 {
		return nil, ErrInvalidRoomName
	}
	if utf8.RuneCountInString(topic) > 255 {
		return nil, ErrInvalidRoomTopic
	}
	creator, ok := s.store.(RoomCreator)
	if !ok {
		return nil, fmt.Errorf("room store does not support create")
	}
	room := &model.Room{ID: uuid.NewString(), Name: name, Topic: topic, CreatorID: creatorID, IsPreset: false}
	if err := creator.Create(ctx, room); err != nil {
		return nil, fmt.Errorf("create room: %w", err)
	}
	return room, nil
}
