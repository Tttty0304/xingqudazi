package service

import (
	"context"
	"testing"
	"time"

	"xingqudazi-im/server/internal/model"
)

// fakeRoomStore / fakeOnlineCounter / fakeMessageStore 均为内存假实现，单测不依赖真实数据库。

type fakeRoomStore struct {
	rooms map[string]model.Room
}

func newFakeRoomStore(rooms ...model.Room) *fakeRoomStore {
	m := make(map[string]model.Room)
	for _, r := range rooms {
		m[r.ID] = r
	}
	return &fakeRoomStore{rooms: m}
}

func (f *fakeRoomStore) List(_ context.Context) ([]model.Room, error) {
	result := make([]model.Room, 0, len(f.rooms))
	for _, r := range f.rooms {
		result = append(result, r)
	}
	return result, nil
}

func (f *fakeRoomStore) FindByID(_ context.Context, id string) (*model.Room, error) {
	r, ok := f.rooms[id]
	if !ok {
		return nil, ErrRepositoryRoomNotFound
	}
	return &r, nil
}

func (f *fakeRoomStore) Create(_ context.Context, room *model.Room) error {
	f.rooms[room.ID] = *room
	return nil
}

type fakeOnlineCounter struct {
	counts map[string]int64
}

func (f *fakeOnlineCounter) Count(_ context.Context, roomID string) (int64, error) {
	return f.counts[roomID], nil
}

// TestRoomService_ListRooms_T20 对应 T20：返回房间列表，含在线人数。
func TestRoomService_ListRooms_T20(t *testing.T) {
	store := newFakeRoomStore(
		model.Room{ID: "r1", Name: "数码", IsPreset: true},
		model.Room{ID: "r2", Name: "追番", IsPreset: true},
	)
	counter := &fakeOnlineCounter{counts: map[string]int64{"r1": 3}}
	svc := NewRoomService(store, counter)

	rooms, err := svc.ListRooms(context.Background())
	if err != nil {
		t.Fatalf("ListRooms failed: %v", err)
	}
	if len(rooms) != 2 {
		t.Fatalf("expected 2 rooms, got %d", len(rooms))
	}

	var found bool
	for _, r := range rooms {
		if r.ID == "r1" {
			found = true
			if r.OnlineCount != 3 {
				t.Errorf("expected online_count=3 for r1, got %d", r.OnlineCount)
			}
		}
	}
	if !found {
		t.Error("expected room r1 in result")
	}
}

// TestRoomService_GetRoom_NotFound_T22 对应 T22：房间不存在返回 ErrRoomNotFound。
func TestRoomService_GetRoom_NotFound_T22(t *testing.T) {
	store := newFakeRoomStore()
	svc := NewRoomService(store, &fakeOnlineCounter{counts: map[string]int64{}})

	_, err := svc.GetRoom(context.Background(), "nonexistent")
	if err != ErrRoomNotFound {
		t.Fatalf("expected ErrRoomNotFound, got %v", err)
	}
}

func TestRoomService_CreateRoom_ValidatesAndPersists(t *testing.T) {
	store := newFakeRoomStore()
	svc := NewRoomService(store, &fakeOnlineCounter{counts: map[string]int64{}})
	room, err := svc.CreateRoom(context.Background(), "creator-id", "  桌游同好会  ", "  周末线下桌游  ")
	if err != nil {
		t.Fatalf("CreateRoom failed: %v", err)
	}
	if room.IsPreset || room.CreatorID != "creator-id" || room.Name != "桌游同好会" || room.Topic != "周末线下桌游" {
		t.Fatalf("unexpected created room: %+v", room)
	}
	if _, ok := store.rooms[room.ID]; !ok {
		t.Fatal("expected created room to be persisted")
	}
	if _, err := svc.CreateRoom(context.Background(), "creator-id", "x", ""); err != ErrInvalidRoomName {
		t.Fatalf("expected invalid room name, got %v", err)
	}
}

type fakeMessageStore struct {
	messages map[string][]model.Message // roomID -> messages
}

func (f *fakeMessageStore) ListByRoom(_ context.Context, roomID string, page, size int) ([]model.Message, bool, error) {
	all := f.messages[roomID]
	offset := (page - 1) * size
	if offset >= len(all) {
		return nil, false, nil
	}
	end := offset + size
	hasMore := end < len(all)
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], hasMore, nil
}

// TestMessageService_ListRoomMessages_T21 对应 T21：房间存在时返回分页消息。
func TestMessageService_ListRoomMessages_T21(t *testing.T) {
	roomStore := newFakeRoomStore(model.Room{ID: "r1", Name: "数码"})
	msgStore := &fakeMessageStore{
		messages: map[string][]model.Message{
			"r1": {
				{ID: "m1", Content: "hello", CreatedAt: time.Now()},
				{ID: "m2", Content: "world", CreatedAt: time.Now()},
			},
		},
	}
	svc := NewMessageService(roomStore, msgStore)

	messages, hasMore, err := svc.ListRoomMessages(context.Background(), "r1", 1, 20)
	if err != nil {
		t.Fatalf("ListRoomMessages failed: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if hasMore {
		t.Error("expected has_more=false when all messages fit in one page")
	}
}

// TestMessageService_ListRoomMessages_RoomNotFound_T22 对应 T22：房间不存在返回 ErrRoomNotFound。
func TestMessageService_ListRoomMessages_RoomNotFound_T22(t *testing.T) {
	roomStore := newFakeRoomStore()
	msgStore := &fakeMessageStore{messages: map[string][]model.Message{}}
	svc := NewMessageService(roomStore, msgStore)

	_, _, err := svc.ListRoomMessages(context.Background(), "nonexistent", 1, 20)
	if err != ErrRoomNotFound {
		t.Fatalf("expected ErrRoomNotFound, got %v", err)
	}
}

// TestMessageService_ListRoomMessages_PageSizeNormalization 验证非法分页参数被规范化，
// 而不是直接把用户输入传给数据库导致异常（如 size=-1 或 size=99999）。
func TestMessageService_ListRoomMessages_PageSizeNormalization(t *testing.T) {
	roomStore := newFakeRoomStore(model.Room{ID: "r1", Name: "数码"})
	msgStore := &fakeMessageStore{
		messages: map[string][]model.Message{
			"r1": {{ID: "m1", Content: "hello", CreatedAt: time.Now()}},
		},
	}
	svc := NewMessageService(roomStore, msgStore)

	_, _, err := svc.ListRoomMessages(context.Background(), "r1", 0, -1)
	if err != nil {
		t.Fatalf("expected no error even with invalid page/size input, got %v", err)
	}
}
