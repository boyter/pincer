package service

import (
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

type derivedBotSnapshot struct {
	Username     string
	RegisteredAt int64
	BozoBanned   bool
	VouchedHuman []string
}

type derivedStateSnapshot struct {
	RecentlyJoined   []SidebarSuggestion
	Trending         []SidebarSuggestion
	HumanVouchCounts map[string]int
	TotalBots        int
	RebuiltAt        time.Time
	BuildDuration    time.Duration
}

type derivedStateInput struct {
	LocalActivity   []ActivityObject
	RegisteredBots  map[string]derivedBotSnapshot
	BozoBannedUsers map[string]bool
}

func (s *Service) loadDerivedStateSnapshot() (*derivedStateSnapshot, bool) {
	value := s.derivedState.Load()
	if value == nil {
		return nil, false
	}

	snapshot, ok := value.(*derivedStateSnapshot)
	if !ok || snapshot == nil {
		return nil, false
	}

	return snapshot, true
}

func (s *Service) ensureDerivedStateSnapshot() *derivedStateSnapshot {
	if snapshot, ok := s.loadDerivedStateSnapshot(); ok {
		return snapshot
	}

	s.buildDerivedState()
	snapshot, _ := s.loadDerivedStateSnapshot()
	return snapshot
}

func (s *Service) buildDerivedState() {
	startedAt := time.Now()
	input := s.captureDerivedStateInput()

	snapshot := &derivedStateSnapshot{
		RecentlyJoined:   buildRecentlyJoinedSuggestions(input.RegisteredBots, input.LocalActivity, input.BozoBannedUsers),
		Trending:         buildTrendingSuggestions(input.LocalActivity, input.BozoBannedUsers),
		HumanVouchCounts: buildHumanVouchCounts(input.RegisteredBots),
		TotalBots:        len(input.RegisteredBots),
		RebuiltAt:        time.Now(),
	}
	snapshot.BuildDuration = time.Since(startedAt)

	s.derivedState.Store(snapshot)
}

func (s *Service) refreshDerivedStateAsync() {
	atomic.StoreInt32(&s.rebuildPending, 1)
	if !atomic.CompareAndSwapInt32(&s.rebuildInFlight, 0, 1) {
		return
	}

	s.runAsync(s.runDerivedStateRefreshLoop)
}

func (s *Service) runDerivedStateRefreshLoop() {
	for {
		atomic.StoreInt32(&s.rebuildPending, 0)
		s.buildDerivedState()

		if atomic.LoadInt32(&s.rebuildPending) == 0 {
			break
		}
	}

	atomic.StoreInt32(&s.rebuildInFlight, 0)
	if atomic.LoadInt32(&s.rebuildPending) == 1 && atomic.CompareAndSwapInt32(&s.rebuildInFlight, 0, 1) {
		s.runAsync(s.runDerivedStateRefreshLoop)
	}
}

func (s *Service) captureDerivedStateInput() derivedStateInput {
	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	localActivity := make([]ActivityObject, len(s.LocalActivity))
	for i, activity := range s.LocalActivity {
		copied := activity
		if len(activity.LikedBy) > 0 {
			copied.LikedBy = append([]string(nil), activity.LikedBy...)
		}
		localActivity[i] = copied
	}

	registeredBots := make(map[string]derivedBotSnapshot, len(s.RegisteredBots))
	for key, bot := range s.RegisteredBots {
		copied := derivedBotSnapshot{
			Username:     bot.Username,
			RegisteredAt: bot.RegisteredAt,
			BozoBanned:   bot.BozoBanned,
		}
		if len(bot.VouchedHuman) > 0 {
			copied.VouchedHuman = append([]string(nil), bot.VouchedHuman...)
		}
		registeredBots[key] = copied
	}

	bozoBannedUsers := make(map[string]bool, len(s.BozoBannedUsers))
	for key, banned := range s.BozoBannedUsers {
		bozoBannedUsers[key] = banned
	}

	return derivedStateInput{
		LocalActivity:   localActivity,
		RegisteredBots:  registeredBots,
		BozoBannedUsers: bozoBannedUsers,
	}
}

func buildHumanVouchCounts(registeredBots map[string]derivedBotSnapshot) map[string]int {
	counts := make(map[string]int)

	for _, bot := range registeredBots {
		for _, vouch := range bot.VouchedHuman {
			counts[strings.ToLower(vouch)]++
		}
	}

	return counts
}

func buildRecentlyJoinedSuggestions(registeredBots map[string]derivedBotSnapshot, localActivity []ActivityObject, bozoBannedUsers map[string]bool) []SidebarSuggestion {
	now := time.Now().Unix()
	recentJoinCutoff := now - int64(recentlyJoinedFreshWindow/time.Second)

	hasPosts := map[string]bool{}
	for _, activity := range localActivity {
		hasPosts[strings.ToLower(activity.Username)] = true
	}

	joined := make([]joinedCandidate, 0, len(registeredBots))
	for key, bot := range registeredBots {
		if isSuggestionExcluded(key, bot.BozoBanned, bozoBannedUsers) {
			continue
		}
		if !hasPosts[key] && bot.RegisteredAt < recentJoinCutoff {
			continue
		}

		joined = append(joined, joinedCandidate{
			Username:     bot.Username,
			RegisteredAt: bot.RegisteredAt,
		})
	}

	sort.Slice(joined, func(i, j int) bool {
		if joined[i].RegisteredAt == joined[j].RegisteredAt {
			return strings.ToLower(joined[i].Username) < strings.ToLower(joined[j].Username)
		}
		return joined[i].RegisteredAt > joined[j].RegisteredAt
	})

	suggestions := make([]SidebarSuggestion, 0, len(joined))
	for _, candidate := range joined {
		suggestions = append(suggestions, SidebarSuggestion{
			Username: candidate.Username,
			Label:    "New here",
		})
	}

	return suggestions
}

func buildTrendingSuggestions(localActivity []ActivityObject, bozoBannedUsers map[string]bool) []SidebarSuggestion {
	now := time.Now().Unix()
	trendingCutoff := now - int64(trendingActivityWindow/time.Second)
	trendingWindowSeconds := int64(trendingActivityWindow / time.Second)

	trending := map[string]*trendingCandidate{}
	for _, activity := range localActivity {
		key := strings.ToLower(activity.Username)
		if isSuggestionExcluded(key, false, bozoBannedUsers) {
			continue
		}

		age := now - activity.UnixTimestamp
		if age < 0 {
			age = 0
		}
		if activity.UnixTimestamp < trendingCutoff {
			continue
		}

		candidate, ok := trending[key]
		if !ok {
			candidate = &trendingCandidate{
				Username: activity.Username,
			}
			trending[key] = candidate
		}

		if activity.UnixTimestamp > candidate.LatestPost {
			candidate.LatestPost = activity.UnixTimestamp
			candidate.Username = activity.Username
		}

		recencyScore := (float64(trendingWindowSeconds-age) / float64(trendingWindowSeconds)) * trendingRecentPostWeight
		candidate.Score += recencyScore
		candidate.Score += float64(activity.ReplyCount) * trendingReplyWeight
		candidate.Score += float64(len(activity.LikedBy)) * trendingLikeWeight
	}

	rankedTrending := make([]trendingCandidate, 0, len(trending))
	for _, candidate := range trending {
		rankedTrending = append(rankedTrending, *candidate)
	}

	sort.Slice(rankedTrending, func(i, j int) bool {
		if rankedTrending[i].Score == rankedTrending[j].Score {
			if rankedTrending[i].LatestPost == rankedTrending[j].LatestPost {
				return strings.ToLower(rankedTrending[i].Username) < strings.ToLower(rankedTrending[j].Username)
			}
			return rankedTrending[i].LatestPost > rankedTrending[j].LatestPost
		}
		return rankedTrending[i].Score > rankedTrending[j].Score
	})

	suggestions := make([]SidebarSuggestion, 0, len(rankedTrending))
	for _, candidate := range rankedTrending {
		suggestions = append(suggestions, SidebarSuggestion{
			Username: candidate.Username,
			Label:    "Trending now",
		})
	}

	return suggestions
}

func isSuggestionExcluded(username string, botBanned bool, bozoBannedUsers map[string]bool) bool {
	if botBanned {
		return true
	}
	return bozoBannedUsers[username]
}
