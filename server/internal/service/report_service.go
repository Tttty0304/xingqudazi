package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"xingqudazi-im/server/internal/model"
)

// ReportStore 是 ReportService 依赖的最小数据访问接口，真实实现见
// repository.ReportRepository。FindExisting 用于 T80 边界要求的幂等：
// 同一举报人对同一目标重复举报，不重复计数（但也不报错，返回已有记录）。
type ReportStore interface {
	Create(ctx context.Context, r *model.Report) error
	FindExisting(ctx context.Context, reporterID, targetType, targetID string) (*model.Report, error)
}

// MessageExistenceChecker/DirectMessageExistenceChecker/UserExistenceChecker 供
// ReportService 校验举报目标是否真实存在（T80：目标不存在 404）。
// 复用既有 repository 的读方法即可满足接口，不需要新增专门方法。
type MessageExistenceChecker interface {
	Exists(ctx context.Context, id string) (bool, error)
}

var validReportTargetTypes = map[string]bool{
	"message":        true,
	"direct_message": true,
	"user":           true,
}

type ReportService struct {
	store    ReportStore
	messages MessageExistenceChecker
	dms      MessageExistenceChecker
	users    UserStore
}

func NewReportService(store ReportStore, messages, dms MessageExistenceChecker, users UserStore) *ReportService {
	return &ReportService{store: store, messages: messages, dms: dms, users: users}
}

// CreateReport 对应 T80：举报消息/私聊消息/用户。
func (s *ReportService) CreateReport(ctx context.Context, reporterID, targetType, targetID, reason string) (*model.Report, error) {
	if !validReportTargetTypes[targetType] {
		return nil, ErrInvalidReportTargetType
	}

	exists, err := s.checkTargetExists(ctx, targetType, targetID)
	if err != nil {
		return nil, fmt.Errorf("check report target exists: %w", err)
	}
	if !exists {
		return nil, ErrReportTargetNotFound
	}

	// T80 边界：重复举报同一对象记录但不重复计数——实现为幂等返回已有记录，
	// 不新增第二条重复行，也不报错。
	if existing, err := s.store.FindExisting(ctx, reporterID, targetType, targetID); err == nil && existing != nil {
		return existing, nil
	}

	report := &model.Report{
		ID:         uuid.NewString(),
		ReporterID: reporterID,
		TargetType: targetType,
		TargetID:   targetID,
		Reason:     reason,
		Status:     "open",
	}
	if err := s.store.Create(ctx, report); err != nil {
		return nil, fmt.Errorf("create report: %w", err)
	}
	return report, nil
}

func (s *ReportService) checkTargetExists(ctx context.Context, targetType, targetID string) (bool, error) {
	switch targetType {
	case "message":
		return s.messages.Exists(ctx, targetID)
	case "direct_message":
		return s.dms.Exists(ctx, targetID)
	case "user":
		_, err := s.users.FindByID(ctx, targetID)
		if err != nil {
			if errors.Is(err, ErrRepositoryUserNotFound) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	default:
		return false, ErrInvalidReportTargetType
	}
}
