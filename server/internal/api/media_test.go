package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/service"
)

func newMediaTestRouter(maxSizeBytes int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	store := newFakeMediaStore()
	uploadDir, err := os.MkdirTemp("", "media_test_upload_*")
	if err != nil {
		panic(err)
	}
	svc := service.NewMediaService(store, uploadDir, maxSizeBytes)
	h := &MediaHandler{MediaService: svc}

	r := gin.New()
	r.Use(fakeAuthMiddleware("alice"))
	r.POST("/api/media/upload", h.Upload)
	return r
}

// buildMultipartUpload 构造一个 multipart/form-data 请求体，字段名 "file"，
// 模拟浏览器 <input type="file"> 上传的真实请求格式。
func buildMultipartUpload(t *testing.T, filename, contentType string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="file"; filename="` + filename + `"`},
		"Content-Type":        {contentType},
	})
	if err != nil {
		t.Fatalf("create multipart part failed: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart content failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer failed: %v", err)
	}
	return body, writer.FormDataContentType()
}

// TestMediaUpload_Success 覆盖 T90：合法图片上传成功。
func TestMediaUpload_Success(t *testing.T) {
	router := newMediaTestRouter(10 * 1024 * 1024)
	body, contentType := buildMultipartUpload(t, "photo.png", "image/png", []byte("fake-png-bytes"))

	req := httptest.NewRequest("POST", "/api/media/upload", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"url":"/uploads/`) {
		t.Fatalf("expected uploads URL in response, got %s", w.Body.String())
	}
}

// TestMediaUpload_UnsupportedType 覆盖 T91：非图片类型（如 text/plain）应被拒绝。
func TestMediaUpload_UnsupportedType(t *testing.T) {
	router := newMediaTestRouter(10 * 1024 * 1024)
	body, contentType := buildMultipartUpload(t, "notes.txt", "text/plain", []byte("hello"))

	req := httptest.NewRequest("POST", "/api/media/upload", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"error":"unsupported_media_type"`) {
		t.Fatalf("expected unsupported_media_type, got %s", w.Body.String())
	}
}

// TestMediaUpload_FileTooLarge 覆盖 T92：超过服务端限制大小的文件应被拒绝。
func TestMediaUpload_FileTooLarge(t *testing.T) {
	router := newMediaTestRouter(10) // 极小的限制，便于测试用小内容触发超限
	body, contentType := buildMultipartUpload(t, "big.png", "image/png", bytes.Repeat([]byte("a"), 1000))

	req := httptest.NewRequest("POST", "/api/media/upload", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"error":"file_too_large"`) {
		t.Fatalf("expected file_too_large, got %s", w.Body.String())
	}
}

// TestMediaUpload_MissingFile 覆盖请求中没有 "file" 字段时的 400。
func TestMediaUpload_MissingFile(t *testing.T) {
	router := newMediaTestRouter(10 * 1024 * 1024)

	req := httptest.NewRequest("POST", "/api/media/upload", bytes.NewBufferString(""))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"error":"missing_file"`) {
		t.Fatalf("expected missing_file, got %s", w.Body.String())
	}
}
