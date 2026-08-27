package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"xingqudazi-im/server/internal/model"
)

// MediaStore 是 MediaService 依赖的最小数据访问接口，真实实现见
// repository.MediaAssetRepository。
type MediaStore interface {
	Create(ctx context.Context, m *model.MediaAsset) error
}

// allowedImageMimeTypes 是 Task16/P0 仅支持的图片 MIME 类型集合；语音/文件为 P1，
// 本次不接受上传（Plan Part3 已明确范围边界）。
var allowedImageMimeTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// MediaService 负责 Task16/T90-T92 的图片上传业务逻辑：类型/大小校验 -> 本地磁盘落盘
// -> media_assets 落库 -> 返回可直接引用的 URL。
// 存储选型：demo/评估项目用本地磁盘 + gin.Static 提供访问，不引入对象存储依赖
// （生产环境应替换为真实对象存储，已在 docs 中如实标注为简化点）。
type MediaService struct {
	store        MediaStore
	uploadDir    string
	maxSizeBytes int64
}

func NewMediaService(store MediaStore, uploadDir string, maxSizeBytes int64) *MediaService {
	return &MediaService{store: store, uploadDir: uploadDir, maxSizeBytes: maxSizeBytes}
}

// UploadImage 对应 T90（合法图片上传）+ T91（非图片类型拒绝）+ T92（超大文件拒绝）。
// declaredSize 来自客户端上传的文件头声明大小，用于提前拒绝明显超限的请求；
// 写入阶段额外用 io.CopyN 限制实际读取字节数，防止伪造 Content-Length 绕过校验。
func (s *MediaService) UploadImage(ctx context.Context, ownerID, mimeType string, declaredSize int64, reader io.Reader) (*model.MediaAsset, error) {
	ext, ok := allowedImageMimeTypes[mimeType]
	if !ok {
		return nil, ErrUnsupportedMediaType
	}
	if declaredSize > s.maxSizeBytes {
		return nil, ErrFileTooLarge
	}

	if err := os.MkdirAll(s.uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("ensure upload dir: %w", err)
	}

	id := uuid.NewString()
	filename := id + ext
	fullPath := filepath.Join(s.uploadDir, filename)

	f, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("create upload file: %w", err)
	}
	defer f.Close()

	// 限制实际写入字节数为 maxSizeBytes+1：若能读出超过上限的字节，说明声明大小与
	// 真实内容不符（或声明本就超限但仍走到这里），判定为超限并清理已写入的半成品文件。
	written, err := io.CopyN(f, reader, s.maxSizeBytes+1)
	if err != nil && err != io.EOF {
		os.Remove(fullPath)
		return nil, fmt.Errorf("write upload file: %w", err)
	}
	if written > s.maxSizeBytes {
		os.Remove(fullPath)
		return nil, ErrFileTooLarge
	}

	asset := &model.MediaAsset{
		ID:        id,
		OwnerID:   ownerID,
		MediaType: "image",
		URL:       "/uploads/" + filename,
		MimeType:  mimeType,
		SizeBytes: written,
	}
	if err := s.store.Create(ctx, asset); err != nil {
		os.Remove(fullPath)
		return nil, fmt.Errorf("persist media asset: %w", err)
	}
	return asset, nil
}
