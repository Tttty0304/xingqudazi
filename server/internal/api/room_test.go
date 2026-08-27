package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
)

// decodeMessagesResponse 解析 {"messages":[...],"has_more":bool} 响应体，
// 用真正的 JSON 解析而非字符串匹配来断言字段值——Go 的 encoding/json 默认会
// 把 `<`/`>`/`&` 转成 `\uXXXX` 转义序列（HTML-safe 输出），字符串层面直接
// 匹配 "&lt;" 这类 html.EscapeString 产物在双重编码后会失真。
func decodeMessagesResponse(t *testing.T, body []byte) ([]map[string]any, bool) {
	t.Helper()
	var parsed struct {
		Messages []map[string]any `json:"messages"`
		HasMore  bool             `json:"has_more"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("failed to parse response JSON: %v, body=%s", err, body)
	}
	return parsed.Messages, parsed.HasMore
}

func newRoomTestRouter(rooms []model.Room, counts map[string]int64, messages []model.Message, hasMore bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	roomStore := newFakeRoomStore(rooms...)
	roomSvc := service.NewRoomService(roomStore, &fakeOnlineCounter{counts: counts})
	msgSvc := service.NewMessageService(roomStore, &fakeMessageStore{messages: messages, hasMore: hasMore})
	h := &RoomHandler{RoomService: roomSvc, MessageService: msgSvc}

	r := gin.New()
	r.GET("/api/rooms", h.ListRooms)
	r.GET("/api/rooms/:id/messages", h.ListRoomMessages)
	r.POST("/api/rooms", func(c *gin.Context) {
		c.Set("auth_user_id", "creator-id")
		h.Create(c)
	})
	return r
}

// TestListRooms_ReturnsOnlineCounts 覆盖 T20：房间列表携带实时在线人数，
// 无需鉴权即可访问。
func TestListRooms_ReturnsOnlineCounts(t *testing.T) {
	roomID := uuid.NewString()
	router := newRoomTestRouter(
		[]model.Room{{ID: roomID, Name: "数码", Topic: "科技话题"}},
		map[string]int64{roomID: 7},
		nil, false,
	)

	req := httptest.NewRequest("GET", "/api/rooms", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"online_count":7`) {
		t.Fatalf("expected online_count=7, got %s", w.Body.String())
	}
}

func TestCreateRoom_ValidatesAndReturnsCreatedRoom(t *testing.T) {
	router := newRoomTestRouter(nil, nil, nil, false)
	req := httptest.NewRequest("POST", "/api/rooms", strings.NewReader(`{"name":"桌游同好会","topic":"周末桌游"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated || !contains(w.Body.String(), `"name":"桌游同好会"`) {
		t.Fatalf("expected created room, got code=%d body=%s", w.Code, w.Body.String())
	}

	bad := httptest.NewRequest("POST", "/api/rooms", strings.NewReader(`{"name":"x"}`))
	bad.Header.Set("Content-Type", "application/json")
	badW := httptest.NewRecorder()
	router.ServeHTTP(badW, bad)
	if badW.Code != http.StatusBadRequest || !contains(badW.Body.String(), `"invalid_room_name"`) {
		t.Fatalf("expected invalid_room_name, got code=%d body=%s", badW.Code, badW.Body.String())
	}
}

// TestListRoomMessages_InvalidRoomIDFormat 覆盖非 UUID 格式的房间 ID 应在到达
// service 层之前被 400 拦截（而不是让数据库驱动报格式错误）。
func TestListRoomMessages_InvalidRoomIDFormat(t *testing.T) {
	router := newRoomTestRouter(nil, nil, nil, false)

	req := httptest.NewRequest("GET", "/api/rooms/not-a-uuid/messages", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"invalid_room_id"`) {
		t.Fatalf("expected invalid_room_id, got %s", w.Body.String())
	}
}

// TestListRoomMessages_RoomNotFound 覆盖合法 UUID 格式但房间不存在时的 404。
func TestListRoomMessages_RoomNotFound(t *testing.T) {
	router := newRoomTestRouter(nil, nil, nil, false)

	req := httptest.NewRequest("GET", "/api/rooms/"+uuid.NewString()+"/messages", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"room_not_found"`) {
		t.Fatalf("expected room_not_found, got %s", w.Body.String())
	}
}

// TestListRoomMessages_XSSEscaped 覆盖 T50：文本消息内容对外输出时必须做 HTML
// 转义，防止存储型 XSS（消息内容含 <script> 标签时应被转义而非原样输出）。
func TestListRoomMessages_XSSEscaped(t *testing.T) {
	roomID := uuid.NewString()
	router := newRoomTestRouter(
		[]model.Room{{ID: roomID, Name: "数码"}},
		nil,
		[]model.Message{{
			ID: uuid.NewString(), MsgID: uuid.NewString(), RoomID: roomID,
			SenderID: "u1", SenderType: "human",
			Content: `<script>alert(1)</script>`, ContentType: "text",
			CreatedAt: time.Now(),
		}},
		false,
	)

	req := httptest.NewRequest("GET", "/api/rooms/"+roomID+"/messages", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}
	messages, _ := decodeMessagesResponse(t, w.Body.Bytes())
	if len(messages) != 1 {
		t.Fatalf("expected exactly 1 message, got %d", len(messages))
	}
	got := messages[0]["content"]
	if got != "&lt;script&gt;alert(1)&lt;/script&gt;" {
		t.Fatalf("expected HTML-escaped content, got %q", got)
	}
}

// TestListRoomMessages_ImageContentNotEscaped 覆盖图片消息的 Content（媒体 URL）
// 不应被 HTML 转义（转义会破坏 URL 里的 & 等字符，导致前端拿到错误的图片地址）。
func TestListRoomMessages_ImageContentNotEscaped(t *testing.T) {
	roomID := uuid.NewString()
	imageURL := "/uploads/abc.jpg?token=a&b=c"
	router := newRoomTestRouter(
		[]model.Room{{ID: roomID, Name: "数码"}},
		nil,
		[]model.Message{{
			ID: uuid.NewString(), MsgID: uuid.NewString(), RoomID: roomID,
			SenderID: "u1", SenderType: "human",
			Content: imageURL, ContentType: "image",
			CreatedAt: time.Now(),
		}},
		false,
	)

	req := httptest.NewRequest("GET", "/api/rooms/"+roomID+"/messages", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	messages, _ := decodeMessagesResponse(t, w.Body.Bytes())
	if len(messages) != 1 {
		t.Fatalf("expected exactly 1 message, got %d", len(messages))
	}
	if got := messages[0]["content"]; got != imageURL {
		t.Fatalf("expected raw image URL to be preserved (not HTML-escaped), got %q", got)
	}
}

// TestListRoomMessages_HasMoreFlag 覆盖分页 has_more 标记正确透传。
func TestListRoomMessages_HasMoreFlag(t *testing.T) {
	roomID := uuid.NewString()
	router := newRoomTestRouter([]model.Room{{ID: roomID, Name: "数码"}}, nil, nil, true)

	req := httptest.NewRequest("GET", "/api/rooms/"+roomID+"/messages", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if !contains(w.Body.String(), `"has_more":true`) {
		t.Fatalf("expected has_more=true, got %s", w.Body.String())
	}
}
