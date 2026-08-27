package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"xingqudazi-im/server/internal/model"
)

// MatchCandidateStore 是 RecommendationService 依赖的最小数据访问接口，真实实现见
// repository.MatchCandidateRepository。
type MatchCandidateStore interface {
	Create(ctx context.Context, m *model.MatchCandidate) (inserted bool, err error)
	ListPendingByUser(ctx context.Context, userID string) ([]model.MatchCandidate, error)
	FindByID(ctx context.Context, id string) (*model.MatchCandidate, error)
	UpdateStatus(ctx context.Context, id, newStatus string) (updated bool, err error)
}

// WatchTopicLister 供 RecommendationService 生成候选时全量扫描关注事项，复用
// repository.WatchTopicRepository.ListAll，不新增专门接口。
type WatchTopicLister interface {
	ListAll(ctx context.Context) ([]model.WatchTopic, error)
}

// FriendChecker（复用 conversation_service.go 中已定义的同名接口，两者结构完全一致：
// 供生成候选时排除已是好友的用户对，推荐已认识的人没有意义）在此处不重复声明。

// RecommendationService 对应 Task20：基于 Task19 关注事项数据的规则化匹配演示。
// 规则（对应 Plan「基于 user_watch_topics 关键词重合+共同房间打分」）：
//   - 两个用户的关注事项关键词存在交集 -> 每个共同关键词加 2 分；
//   - 两个用户在同一房间维度都设置了关注事项（room_id 相同）-> 额外加 1 分；
//   - 已是好友的用户对不生成候选（推荐目的是"认识新朋友"，不是好友列表的重复展示）。
type RecommendationService struct {
	candidates MatchCandidateStore
	topics     WatchTopicLister
	friends    FriendChecker
	users      UserStore
}

func NewRecommendationService(candidates MatchCandidateStore, topics WatchTopicLister, friends FriendChecker, users UserStore) *RecommendationService {
	return &RecommendationService{candidates: candidates, topics: topics, friends: friends, users: users}
}

// splitKeywords 把逗号分隔的关键词字符串拆分为去重后的小写词集合，用于关键词重合判断。
func splitKeywords(raw string) map[string]bool {
	set := make(map[string]bool)
	for _, w := range strings.Split(raw, ",") {
		w = strings.TrimSpace(strings.ToLower(w))
		if w != "" {
			set[w] = true
		}
	}
	return set
}

// GenerateCandidates 对应 T110：扫描全部关注事项，两两用户配对计算规则化匹配分，
// 写入 match_candidates（已存在的候选对不重复插入，见 repository 层 ON CONFLICT
// DO NOTHING，因此本方法可安全重复调用，不会产生重复推荐）。返回本次新增的候选数。
func (s *RecommendationService) GenerateCandidates(ctx context.Context) (created int, err error) {
	topics, err := s.topics.ListAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("list watch topics: %w", err)
	}

	// 按用户分组，避免 O(n^2) 里重复拆分同一用户的关键词集合。
	byUser := make(map[string][]model.WatchTopic)
	var userIDs []string
	seen := make(map[string]bool)
	for _, t := range topics {
		byUser[t.UserID] = append(byUser[t.UserID], t)
		if !seen[t.UserID] {
			seen[t.UserID] = true
			userIDs = append(userIDs, t.UserID)
		}
	}

	for i := 0; i < len(userIDs); i++ {
		for j := i + 1; j < len(userIDs); j++ {
			userA, userB := userIDs[i], userIDs[j]
			// 规范化排序，与表上 UNIQUE (user_a_id, user_b_id, room_id) 约束的方向保持一致，
			// 避免 (A,B) 和 (B,A) 被当成两条不同的候选。
			if userA > userB {
				userA, userB = userB, userA
			}

			isFriend, err := s.friends.IsFriend(ctx, userA, userB)
			if err != nil {
				return created, fmt.Errorf("check friendship for %s/%s: %w", userA, userB, err)
			}
			if isFriend {
				continue
			}

			sharedKeywords, sharedRoomID := matchTopics(byUser[userIDs[i]], byUser[userIDs[j]])
			if len(sharedKeywords) == 0 && sharedRoomID == "" {
				continue
			}

			score := float64(len(sharedKeywords) * 2)
			reasonParts := make([]string, 0, 2)
			sharedTopic := ""
			if len(sharedKeywords) > 0 {
				sharedTopic = sharedKeywords[0]
				reasonParts = append(reasonParts, "你们都关注："+strings.Join(sharedKeywords, "、"))
			}
			if sharedRoomID != "" {
				score += 1
				reasonParts = append(reasonParts, "且都在同一个兴趣房间设置了关注事项")
			}

			candidate := &model.MatchCandidate{
				ID:          uuid.NewString(),
				UserAID:     userA,
				UserBID:     userB,
				SharedTopic: sharedTopic,
				RoomID:      sharedRoomID,
				MatchReason: strings.Join(reasonParts, "，"),
				MatchScore:  score,
				Status:      "pending_review",
			}
			inserted, err := s.candidates.Create(ctx, candidate)
			if err != nil {
				return created, fmt.Errorf("create match candidate for %s/%s: %w", userA, userB, err)
			}
			if inserted {
				created++
			}
		}
	}
	return created, nil
}

// matchTopics 计算两个用户关注事项列表之间的共同关键词（并集去重后的交集，跨房间
// 汇总）与"共同房间"信号（任意一对 room_id 相同且非空时命中，取第一个命中的 room_id）。
func matchTopics(topicsA, topicsB []model.WatchTopic) (sharedKeywords []string, sharedRoomID string) {
	setA := make(map[string]bool)
	for _, t := range topicsA {
		for w := range splitKeywords(t.Keywords) {
			setA[w] = true
		}
	}

	sharedSet := make(map[string]bool)
	for _, t := range topicsB {
		for w := range splitKeywords(t.Keywords) {
			if setA[w] {
				sharedSet[w] = true
			}
		}
	}
	for w := range sharedSet {
		sharedKeywords = append(sharedKeywords, w)
	}

	for _, ta := range topicsA {
		if ta.RoomID == "" {
			continue
		}
		for _, tb := range topicsB {
			if tb.RoomID == ta.RoomID {
				return sharedKeywords, ta.RoomID
			}
		}
	}
	return sharedKeywords, ""
}

// RecommendationCandidate 是 GET /api/recommendations 的响应形态，已把"对方"从
// user_a_id/user_b_id 中解析出来，调用方（api 层）无需重复判断方向。
type RecommendationCandidate struct {
	CandidateID  string
	PeerID       string
	PeerUsername string
	SharedTopic  string
	RoomID       string
	MatchReason  string
	MatchScore   float64
}

// ListCandidates 对应 T111：查询当前用户的全部待确认推荐候选。
func (s *RecommendationService) ListCandidates(ctx context.Context, userID string) ([]RecommendationCandidate, error) {
	rows, err := s.candidates.ListPendingByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list pending candidates: %w", err)
	}

	result := make([]RecommendationCandidate, 0, len(rows))
	for _, m := range rows {
		peerID := m.UserBID
		if m.UserAID != userID {
			peerID = m.UserAID
		}
		peer, err := s.users.FindByID(ctx, peerID)
		if err != nil {
			continue // 对端用户异常不应导致整个列表失败，与 FriendService.ListFriends 同款容错处理
		}
		result = append(result, RecommendationCandidate{
			CandidateID:  m.ID,
			PeerID:       peer.ID,
			PeerUsername: peer.Username,
			SharedTopic:  m.SharedTopic,
			RoomID:       m.RoomID,
			MatchReason:  m.MatchReason,
			MatchScore:   m.MatchScore,
		})
	}
	return result, nil
}

// candidateAction 校验并规范化 T112 的 action 字段，与 friendRequestAction 同款设计。
func candidateAction(action string) (status string, ok bool) {
	switch action {
	case "confirm":
		return "confirmed", true
	case "dismiss":
		return "dismissed", true
	default:
		return "", false
	}
}

// RespondCandidate 对应 T112：确认或忽略一条推荐候选。actorID 必须是候选双方之一。
func (s *RecommendationService) RespondCandidate(ctx context.Context, candidateID, actorID, action string) error {
	newStatus, ok := candidateAction(action)
	if !ok {
		return ErrInvalidCandidateAction
	}

	candidate, err := s.candidates.FindByID(ctx, candidateID)
	if err != nil {
		if errors.Is(err, ErrRepositoryCandidateNotFound) {
			return ErrCandidateNotFound
		}
		return fmt.Errorf("find candidate: %w", err)
	}

	if candidate.UserAID != actorID && candidate.UserBID != actorID {
		return ErrForbiddenCandidateRespond
	}
	if candidate.Status != "pending_review" {
		return ErrCandidateAlreadyResolved
	}

	updated, err := s.candidates.UpdateStatus(ctx, candidateID, newStatus)
	if err != nil {
		return fmt.Errorf("update candidate status: %w", err)
	}
	if !updated {
		return ErrCandidateAlreadyResolved
	}
	return nil
}
