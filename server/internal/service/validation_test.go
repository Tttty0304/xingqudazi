package service

import "testing"

func TestValidateUsername(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", "alice_01", false},
		{"too_short", "ab", true},
		{"too_long", "a_very_long_username_that_exceeds_32_chars", true},
		{"invalid_char_space", "alice bob", true},
		{"invalid_char_special", "alice@bob", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUsername(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateUsername(%q) error=%v, wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}

// TestValidatePassword 对应 Testcase T12：密码过短（如 "123"）应被拒绝；
// 能力补齐后新增覆盖密码复杂度要求（必须同时包含字母与数字）。
func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", "Passw0rd!", false},
		{"too_short_matches_T12", "123", true},
		{"exactly_min_length_with_complexity", "abcd1234", false},
		{"one_below_min", "abcd123", true},
		{"digits_only_rejected", "12345678", true},
		{"letters_only_rejected", "abcdefgh", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidatePassword(%q) error=%v, wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}
