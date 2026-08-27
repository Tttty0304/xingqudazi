package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type strictBindFixture struct {
	Name string `json:"name"`
}

type strictRequiredBindFixture struct {
	Name string `json:"name" binding:"required"`
}

func TestBindJSONStrict(t *testing.T) {
	cases := []struct {
		name, body string
		wantErr    bool
	}{
		{"valid", `{"name":"ok"}`, false},
		{"unknown_field", `{"name":"ok","typo":true}`, true},
		{"wrong_type", `{"name":1}`, true},
		{"multiple_values", `{"name":"ok"} {}`, true},
		{"empty", ``, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/", http.NoBody)
			c.Request.Body = ioNopCloser(tc.body)
			var target strictBindFixture
			if gotErr := bindJSONStrict(c, &target) != nil; gotErr != tc.wantErr {
				t.Fatalf("error=%v want=%v", gotErr, tc.wantErr)
			}
		})
	}
}

func TestBindJSONStrict_EnforcesBindingTags(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", ioNopCloser(`{}`))
	var target strictRequiredBindFixture
	if err := bindJSONStrict(c, &target); err == nil {
		t.Fatal("missing required field should be rejected")
	}
}

func TestParsePageRejectsMalformedAndOutOfRangeValues(t *testing.T) {
	for _, raw := range []string{"page=abc", "page=0", "size=0", "size=101"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/?"+raw, nil)
		_, _, ok := parsePage(c)
		if ok {
			t.Fatalf("%q should be rejected", raw)
		}
	}
}

// ioNopCloser keeps the test independent from private helper implementations.
func ioNopCloser(body string) io.ReadCloser { return io.NopCloser(strings.NewReader(body)) }
