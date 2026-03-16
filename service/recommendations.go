package service

import (
	"sort"
	"strings"
	"time"
)

const (
	defaultWhoToFollowLimit   = 3
	recentlyJoinedFreshWindow = 7 * 24 * time.Hour
	trendingActivityWindow    = 24 * time.Hour
	trendingRecentPostWeight  = 6.0
	trendingReplyWeight       = 3.0
	trendingLikeWeight        = 2.0
)

type SidebarSuggestion struct {
	Username string
	Label    string
}

type SidebarSuggestions struct {
	RecentlyJoined []SidebarSuggestion
	Trending       []SidebarSuggestion
}

type joinedCandidate struct {
	Username     string
	RegisteredAt int64
}

type trendingCandidate struct {
	Username   string
	LatestPost int64
	Score      float64
}

func (s *Service) GetSidebarSuggestions(limit int) SidebarSuggestions {
	if limit <= 0 {
		limit = defaultWhoToFollowLimit
	}

	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	now := time.Now().Unix()
	recentJoinCutoff := now - int64(recentlyJoinedFreshWindow/time.Second)
	trendingCutoff := now - int64(trendingActivityWindow/time.Second)
	trendingWindowSeconds := int64(trendingActivityWindow / time.Second)

	hasPosts := map[string]bool{}
	for _, activity := range s.LocalActivity {
		hasPosts[strings.ToLower(activity.Username)] = true
	}

	var joined []joinedCandidate
	for key, bot := range s.RegisteredBots {
		if s.isSuggestionExcluded(key, bot.BozoBanned) {
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

	suggestions := SidebarSuggestions{
		RecentlyJoined: make([]SidebarSuggestion, 0, limit),
		Trending:       make([]SidebarSuggestion, 0, limit),
	}
	excluded := map[string]bool{}

	for _, candidate := range joined {
		if len(suggestions.RecentlyJoined) >= limit {
			break
		}
		key := strings.ToLower(candidate.Username)
		excluded[key] = true
		suggestions.RecentlyJoined = append(suggestions.RecentlyJoined, SidebarSuggestion{
			Username: candidate.Username,
			Label:    "New here",
		})
	}

	trending := map[string]*trendingCandidate{}
	for _, activity := range s.LocalActivity {
		key := strings.ToLower(activity.Username)
		if excluded[key] || s.isSuggestionExcluded(key, false) {
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
		candidate.Score += float64(LikeCount(activity)) * trendingLikeWeight
	}

	var rankedTrending []trendingCandidate
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

	for _, candidate := range rankedTrending {
		if len(suggestions.Trending) >= limit {
			break
		}
		suggestions.Trending = append(suggestions.Trending, SidebarSuggestion{
			Username: candidate.Username,
			Label:    "Trending now",
		})
	}

	return suggestions
}

func (s *Service) isSuggestionExcluded(username string, botBanned bool) bool {
	if botBanned {
		return true
	}
	return s.BozoBannedUsers[username]
}
