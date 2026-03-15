package service

import (
	"github.com/boyter/pincer/common"
	"sync"
	"testing"
	"time"
)

func TestGetSidebarSuggestionsRecentlyJoinedSortsNewestFirst(t *testing.T) {
	ser := newRecommendationTestService()
	now := time.Now().Unix()

	ser.RegisteredBots["oldposter"] = &RegisteredBot{Username: "oldposter", RegisteredAt: now - int64(8*24*time.Hour/time.Second)}
	ser.RegisteredBots["newest"] = &RegisteredBot{Username: "newest", RegisteredAt: now - 60}
	ser.RegisteredBots["middle"] = &RegisteredBot{Username: "middle", RegisteredAt: now - 120}
	ser.RegisteredBots["oldnopost"] = &RegisteredBot{Username: "oldnopost", RegisteredAt: now - int64(10*24*time.Hour/time.Second)}
	ser.LocalActivity = []ActivityObject{
		{Username: "oldposter", UnixTimestamp: now - 300},
	}

	suggestions := ser.GetSidebarSuggestions(3)

	if len(suggestions.RecentlyJoined) != 3 {
		t.Fatalf("expected 3 recently joined suggestions, got %d", len(suggestions.RecentlyJoined))
	}
	if suggestions.RecentlyJoined[0].Username != "newest" {
		t.Errorf("expected newest first, got %s", suggestions.RecentlyJoined[0].Username)
	}
	if suggestions.RecentlyJoined[1].Username != "middle" {
		t.Errorf("expected middle second, got %s", suggestions.RecentlyJoined[1].Username)
	}
	if suggestions.RecentlyJoined[2].Username != "oldposter" {
		t.Errorf("expected oldposter third, got %s", suggestions.RecentlyJoined[2].Username)
	}
}

func TestGetSidebarSuggestionsTrendingUsesRecentLikesAndReplies(t *testing.T) {
	ser := newRecommendationTestService()
	now := time.Now().Unix()

	ser.LocalActivity = []ActivityObject{
		{Username: "fresh", UnixTimestamp: now - 120, ReplyCount: 0, LikedBy: []string{}},
		{Username: "popular", UnixTimestamp: now - 600, ReplyCount: 2, LikedBy: []string{"a", "b"}},
		{Username: "stale", UnixTimestamp: now - int64(26*time.Hour/time.Second), ReplyCount: 10, LikedBy: []string{"a", "b", "c"}},
	}

	suggestions := ser.GetSidebarSuggestions(3)

	if len(suggestions.Trending) < 2 {
		t.Fatalf("expected at least 2 trending suggestions, got %d", len(suggestions.Trending))
	}
	if suggestions.Trending[0].Username != "popular" {
		t.Errorf("expected popular first, got %s", suggestions.Trending[0].Username)
	}
	for _, suggestion := range suggestions.Trending {
		if suggestion.Username == "stale" {
			t.Error("did not expect stale user in trending suggestions")
		}
	}
}

func TestGetSidebarSuggestionsExcludesBozoBannedAndDeduplicates(t *testing.T) {
	ser := newRecommendationTestService()
	now := time.Now().Unix()

	ser.RegisteredBots["newbot"] = &RegisteredBot{Username: "newbot", RegisteredAt: now - 60}
	ser.RegisteredBots["bannedbot"] = &RegisteredBot{Username: "bannedbot", RegisteredAt: now - 30, BozoBanned: true}
	ser.LocalActivity = []ActivityObject{
		{Username: "newbot", UnixTimestamp: now - 30, ReplyCount: 2, LikedBy: []string{"fan"}},
		{Username: "bannedbot", UnixTimestamp: now - 30, ReplyCount: 5, LikedBy: []string{"fan1", "fan2"}},
		{Username: "otherbot", UnixTimestamp: now - 45, ReplyCount: 1, LikedBy: []string{"fan"}},
	}
	ser.BozoBannedUsers["bannedbot"] = true

	suggestions := ser.GetSidebarSuggestions(3)

	for _, suggestion := range suggestions.RecentlyJoined {
		if suggestion.Username == "bannedbot" {
			t.Error("did not expect bannedbot in recently joined")
		}
	}
	for _, suggestion := range suggestions.Trending {
		if suggestion.Username == "newbot" {
			t.Error("did not expect newbot in trending when already in recently joined")
		}
		if suggestion.Username == "bannedbot" {
			t.Error("did not expect bannedbot in trending")
		}
	}
}

func TestGetSidebarSuggestionsReturnsShorterListsWhenNeeded(t *testing.T) {
	ser := newRecommendationTestService()
	now := time.Now().Unix()

	ser.RegisteredBots["solo"] = &RegisteredBot{Username: "solo", RegisteredAt: now - 60}
	ser.LocalActivity = []ActivityObject{
		{Username: "solo", UnixTimestamp: now - 60, ReplyCount: 1, LikedBy: []string{"fan"}},
	}

	suggestions := ser.GetSidebarSuggestions(3)

	if len(suggestions.RecentlyJoined) != 1 {
		t.Errorf("expected 1 recently joined suggestion, got %d", len(suggestions.RecentlyJoined))
	}
	if len(suggestions.Trending) != 0 {
		t.Errorf("expected 0 trending suggestions after deduplication, got %d", len(suggestions.Trending))
	}
}

func newRecommendationTestService() *Service {
	return &Service{
		Environment:        &common.Environment{BaseUrl: "http://localhost/", MaxPostLength: 500},
		ServiceMutex:       sync.RWMutex{},
		LocalActivity:      []ActivityObject{},
		UserActivity:       map[string]*ActivityUser{},
		RegisteredBots:     map[string]*RegisteredBot{},
		ApiKeyIndex:        map[string]string{},
		BozoBannedUsers:    map[string]bool{},
		UserPostTimestamps: map[string][]int64{},
	}
}
