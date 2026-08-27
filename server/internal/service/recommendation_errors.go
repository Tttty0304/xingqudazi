package service

import "errors"

// AI 推荐（Task20）相关的语义化错误。
var (
	ErrCandidateNotFound         = errors.New("candidate_not_found")
	ErrForbiddenCandidateRespond = errors.New("forbidden_candidate_respond")
	ErrCandidateAlreadyResolved  = errors.New("candidate_already_resolved")
	ErrInvalidCandidateAction    = errors.New("invalid_candidate_action")

	// ErrRepositoryCandidateNotFound 是 repository 层"未找到"的内部 sentinel，
	// 与 ErrRepositoryFriendshipNotFound/ErrRepositoryUserNotFound 同款约定，
	// service 层据此翻译为对外的 ErrCandidateNotFound。
	ErrRepositoryCandidateNotFound = errors.New("repository_candidate_not_found")
)
