package ws

import "testing"

func TestDecodeClientMessageStrictAndRecoverable(t *testing.T) {
	roomID := "00000000-0000-0000-0000-000000000001"
	cases := []struct {
		name, raw string
		wantErr   bool
	}{
		{"valid", `{"type":"join_room","room_id":"` + roomID + `"}`, false},
		{"unknown_field", `{"type":"join_room","room_id":"` + roomID + `","typo":true}`, true},
		{"invalid_room_id", `{"type":"join_room","room_id":"not-a-uuid"}`, true},
		{"multiple_json_values", `{"type":"leave_room","room_id":"` + roomID + `"} {}`, true},
		{"unknown_event_is_decoded_for_dispatch_error", `{"type":"wrong_event"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeClientMessage([]byte(tc.raw))
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestBroadcastChannel(t *testing.T) {
	got := broadcastChannel("room-1")
	want := "room:room-1:broadcast"
	if got != want {
		t.Errorf("broadcastChannel(%q) = %q, want %q", "room-1", got, want)
	}
}

func TestOnlineUsersKey(t *testing.T) {
	got := onlineUsersKey("room-1")
	want := "room:room-1:online_users"
	if got != want {
		t.Errorf("onlineUsersKey(%q) = %q, want %q", "room-1", got, want)
	}
}

// TestValidateSendMessage 覆盖 T33-T35 的纯校验逻辑。
func TestValidateSendMessage(t *testing.T) {
	cases := []struct {
		name     string
		msg      ClientMessage
		maxLen   int
		wantCode string
		wantOK   bool
	}{
		{
			name:     "valid_T33",
			msg:      ClientMessage{RoomID: "r1", MsgID: "m1", Content: "hello"},
			maxLen:   1000,
			wantCode: "",
			wantOK:   true,
		},
		{
			name:     "missing_room_id",
			msg:      ClientMessage{Content: "hello"},
			maxLen:   1000,
			wantCode: "invalid_message",
			wantOK:   false,
		},
		{
			name:     "empty_content",
			msg:      ClientMessage{RoomID: "r1", Content: ""},
			maxLen:   1000,
			wantCode: "invalid_message",
			wantOK:   false,
		},
		{
			name:     "content_too_long_T35",
			msg:      ClientMessage{RoomID: "r1", Content: "aaaaaaaaaa"},
			maxLen:   5,
			wantCode: "content_too_long",
			wantOK:   false,
		},
		{
			name:     "content_exactly_at_limit",
			msg:      ClientMessage{RoomID: "r1", Content: "aaaaa"},
			maxLen:   5,
			wantCode: "",
			wantOK:   true,
		},
		{
			name:     "image_content_type_T90_valid",
			msg:      ClientMessage{RoomID: "r1", Content: "/uploads/abc.png", ContentType: "image"},
			maxLen:   1000,
			wantCode: "",
			wantOK:   true,
		},
		{
			name:     "unsupported_content_type_rejected",
			msg:      ClientMessage{RoomID: "r1", Content: "hi", ContentType: "voice"},
			maxLen:   1000,
			wantCode: "unsupported_content_type",
			wantOK:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, ok := validateSendMessage(tc.msg, tc.maxLen)
			if ok != tc.wantOK {
				t.Errorf("validateSendMessage() ok = %v, want %v", ok, tc.wantOK)
			}
			if code != tc.wantCode {
				t.Errorf("validateSendMessage() code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}

// TestNormalizeContentType 覆盖 Task16 起新增的默认值规范化逻辑。
func TestNormalizeContentType(t *testing.T) {
	if got := normalizeContentType(""); got != "text" {
		t.Errorf("normalizeContentType(\"\") = %q, want %q", got, "text")
	}
	if got := normalizeContentType("image"); got != "image" {
		t.Errorf("normalizeContentType(\"image\") = %q, want %q", got, "image")
	}
}

// TestContainsSensitiveWord 覆盖 Task18/T81 的纯校验逻辑：大小写不敏感子串匹配。
func TestContainsSensitiveWord(t *testing.T) {
	words := []string{"badword1", "违禁词示例"}

	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"clean_content_T33", "hello world", false},
		{"hit_lowercase", "this contains badword1 inside", true},
		{"hit_mixed_case_insensitive", "this contains BadWord1 inside", true},
		{"hit_chinese_word", "消息里出现了违禁词示例这种内容", true},
		{"empty_wordlist_never_hits", "badword1", false}, // 用空词表单独验证，见下方子测试
	}

	for _, tc := range cases[:4] {
		t.Run(tc.name, func(t *testing.T) {
			_, blocked := containsSensitiveWord(tc.content, words)
			if blocked != tc.want {
				t.Errorf("containsSensitiveWord(%q) blocked = %v, want %v", tc.content, blocked, tc.want)
			}
		})
	}

	t.Run("empty_wordlist_never_hits", func(t *testing.T) {
		_, blocked := containsSensitiveWord("badword1", nil)
		if blocked {
			t.Error("expected empty sensitive word list to never block content")
		}
	})
}

func TestMustMarshal_ValidPayload(t *testing.T) {
	data := mustMarshal(ServerMessage{Type: EventConnected, UserID: "u1"})
	if len(data) == 0 {
		t.Fatal("expected non-empty marshaled data")
	}
	got := string(data)
	if got == "{}" {
		t.Fatal("expected real payload, not empty object fallback")
	}
}

// TestValidateSendDirectMessage 覆盖 Task15/T70 的纯校验逻辑。
func TestValidateSendDirectMessage(t *testing.T) {
	cases := []struct {
		name     string
		msg      ClientMessage
		maxLen   int
		wantCode string
		wantOK   bool
	}{
		{
			name:     "valid_T70",
			msg:      ClientMessage{TargetUserID: "bob", MsgID: "m1", Content: "hello"},
			maxLen:   1000,
			wantCode: "",
			wantOK:   true,
		},
		{
			name:     "missing_target_user_id",
			msg:      ClientMessage{Content: "hello"},
			maxLen:   1000,
			wantCode: "invalid_message",
			wantOK:   false,
		},
		{
			name:     "empty_content",
			msg:      ClientMessage{TargetUserID: "bob", Content: ""},
			maxLen:   1000,
			wantCode: "invalid_message",
			wantOK:   false,
		},
		{
			name:     "content_too_long",
			msg:      ClientMessage{TargetUserID: "bob", Content: "aaaaaaaaaa"},
			maxLen:   5,
			wantCode: "content_too_long",
			wantOK:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, ok := validateSendDirectMessage(tc.msg, tc.maxLen)
			if ok != tc.wantOK {
				t.Errorf("validateSendDirectMessage() ok = %v, want %v", ok, tc.wantOK)
			}
			if code != tc.wantCode {
				t.Errorf("validateSendDirectMessage() code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}
