package service

import (
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

	snapshot := s.ensureDerivedStateSnapshot()
	suggestions := SidebarSuggestions{
		RecentlyJoined: make([]SidebarSuggestion, 0, limit),
		Trending:       make([]SidebarSuggestion, 0, limit),
	}
	if snapshot == nil {
		return suggestions
	}

	excluded := map[string]bool{}
	for _, candidate := range snapshot.RecentlyJoined {
		if len(suggestions.RecentlyJoined) >= limit {
			break
		}
		excluded[strings.ToLower(candidate.Username)] = true
		suggestions.RecentlyJoined = append(suggestions.RecentlyJoined, candidate)
	}

	for _, candidate := range snapshot.Trending {
		if len(suggestions.Trending) >= limit {
			break
		}
		if excluded[strings.ToLower(candidate.Username)] {
			continue
		}
		suggestions.Trending = append(suggestions.Trending, candidate)
	}

	return suggestions
}
