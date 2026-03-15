package service

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestDerivedStateRefreshesAfterCreatePostAndLike(t *testing.T) {
	ser := newRecommendationTestService()
	now := time.Now().Unix()

	ser.LocalActivity = []ActivityObject{
		{Username: "starter", PostId: "starter-1", Id: "starter-1", UnixTimestamp: now - 600},
	}
	ser.buildDerivedState()

	post, err := ser.CreatePost("fresh", "hello world", "")
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}

	waitForDerivedState(t, ser, func(snapshot *derivedStateSnapshot) bool {
		return !containsSuggestion(snapshot.RecentlyJoined, "fresh") && containsSuggestion(snapshot.Trending, "fresh")
	})

	beforeLike, _ := ser.loadDerivedStateSnapshot()
	if err := ser.LikePost(post.PostId, "fan1"); err != nil {
		t.Fatalf("LikePost returned error: %v", err)
	}

	waitForDerivedState(t, ser, func(snapshot *derivedStateSnapshot) bool {
		return snapshot != nil && containsSuggestion(snapshot.Trending, "fresh") && snapshot.RebuiltAt.After(beforeLike.RebuiltAt)
	})
}

func TestDerivedStateRefreshesAfterDeleteAndRegister(t *testing.T) {
	ser := newRecommendationTestService()
	now := time.Now().Unix()

	ser.LocalActivity = []ActivityObject{
		{Username: "removable", PostId: "remove-me", Id: "remove-me", UnixTimestamp: now - 120, ReplyCount: 2, LikedBy: []string{"fan"}},
	}
	ser.RegisteredBots["older"] = &RegisteredBot{Username: "older", RegisteredAt: now - 300}
	ser.buildDerivedState()

	if !ser.DeletePost("remove-me") {
		t.Fatal("expected DeletePost to remove existing post")
	}

	waitForDerivedState(t, ser, func(snapshot *derivedStateSnapshot) bool {
		return !containsSuggestion(snapshot.Trending, "removable")
	})

	if _, err := ser.RegisterBot("newbie"); err != nil {
		t.Fatalf("RegisterBot returned error: %v", err)
	}

	waitForDerivedState(t, ser, func(snapshot *derivedStateSnapshot) bool {
		return len(snapshot.RecentlyJoined) > 0 && snapshot.RecentlyJoined[0].Username == "newbie"
	})
}

func TestDerivedStateRefreshesAfterVouchAndUnvouch(t *testing.T) {
	ser := newRecommendationTestService()
	ser.RegisteredBots["voter"] = &RegisteredBot{Username: "voter", RegisteredAt: time.Now().Unix() - 60}
	ser.RegisteredBots["peer"] = &RegisteredBot{Username: "peer", RegisteredAt: time.Now().Unix() - 120}
	ser.buildDerivedState()

	if err := ser.VouchHuman("voter", "target"); err != nil {
		t.Fatalf("VouchHuman returned error: %v", err)
	}

	waitForDerivedState(t, ser, func(snapshot *derivedStateSnapshot) bool {
		return snapshot.HumanVouchCounts["target"] == 1
	})

	status := ser.GetHumanStatusBatch([]string{"target"})["target"]
	if status.VouchCount != 1 || status.TotalBots != 2 {
		t.Fatalf("unexpected human status after vouch: %+v", status)
	}

	if err := ser.UnvouchHuman("voter", "target"); err != nil {
		t.Fatalf("UnvouchHuman returned error: %v", err)
	}

	waitForDerivedState(t, ser, func(snapshot *derivedStateSnapshot) bool {
		return snapshot.HumanVouchCounts["target"] == 0
	})
}

func TestDerivedStateRefreshesAfterBozoBanAndPurge(t *testing.T) {
	ser := newRecommendationTestService()
	now := time.Now().Unix()

	ser.RegisteredBots["loudbot"] = &RegisteredBot{Username: "loudbot", RegisteredAt: now - 60}
	ser.LocalActivity = []ActivityObject{
		{Username: "loudbot", PostId: "loud-1", Id: "loud-1", UnixTimestamp: now - 30, ReplyCount: 3, LikedBy: []string{"fan"}},
		{Username: "cleanbot", PostId: "clean-1", Id: "clean-1", UnixTimestamp: now - 40},
	}
	ser.buildDerivedState()

	ser.BozoBanUser("loudbot")
	waitForDerivedState(t, ser, func(snapshot *derivedStateSnapshot) bool {
		return !containsSuggestion(snapshot.RecentlyJoined, "loudbot") && !containsSuggestion(snapshot.Trending, "loudbot")
	})

	ser.LocalActivity = append(ser.LocalActivity, ActivityObject{
		Username:      "badcontent",
		PostId:        "bad-1",
		Id:            "bad-1",
		Content:       "you are retarded",
		UnixTimestamp: now - 10,
	})
	ser.UserActivity["badcontent"] = &ActivityUser{
		Name:                         "badcontent",
		Activity:                     []ActivityObject{{Username: "badcontent", PostId: "bad-1", Id: "bad-1", Content: "you are retarded", UnixTimestamp: now - 10}},
		LastInteractionUnixTimestamp: now,
	}
	ser.buildDerivedState()

	purged, banned := ser.PurgeBannedContent()
	if purged == 0 || len(banned) == 0 {
		t.Fatalf("expected PurgeBannedContent to purge and ban content, got purged=%d banned=%v", purged, banned)
	}

	waitForDerivedState(t, ser, func(snapshot *derivedStateSnapshot) bool {
		return !containsSuggestion(snapshot.Trending, "badcontent")
	})
}

func TestGetHumanStatusBatchUsesCachedThresholds(t *testing.T) {
	ser := newRecommendationTestService()
	now := time.Now().Unix()

	for i := 0; i < 10; i++ {
		username := fmt.Sprintf("voter%d", i)
		vouches := []string{}
		if i < 3 {
			vouches = append(vouches, "slight")
		}
		if i < 5 {
			vouches = append(vouches, "likely")
		}
		vouches = append(vouches, "confirmed")

		ser.RegisteredBots[username] = &RegisteredBot{
			Username:     username,
			RegisteredAt: now - int64(i),
			VouchedHuman: vouches,
		}
	}
	ser.buildDerivedState()

	statuses := ser.GetHumanStatusBatch([]string{"slight", "likely", "confirmed", "nobody"})

	if statuses["slight"].Tier != "Slightly Human" {
		t.Fatalf("expected slight to be Slightly Human, got %+v", statuses["slight"])
	}
	if statuses["likely"].Tier != "Likely Human" {
		t.Fatalf("expected likely to be Likely Human, got %+v", statuses["likely"])
	}
	if statuses["confirmed"].Tier != "Confirmed Human" {
		t.Fatalf("expected confirmed to be Confirmed Human, got %+v", statuses["confirmed"])
	}
	if statuses["nobody"].Tier != "" || statuses["nobody"].VouchCount != 0 {
		t.Fatalf("expected nobody to have no tier, got %+v", statuses["nobody"])
	}
}

func TestDerivedStateConcurrentReadersAndWriters(t *testing.T) {
	ser := newRecommendationTestService()
	now := time.Now().Unix()

	ser.RegisteredBots["writer"] = &RegisteredBot{Username: "writer", RegisteredAt: now - 60}
	ser.LocalActivity = []ActivityObject{
		{Username: "writer", PostId: "seed", Id: "seed", UnixTimestamp: now - 30},
	}
	ser.buildDerivedState()

	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			deadline := time.Now().Add(150 * time.Millisecond)
			for time.Now().Before(deadline) {
				suggestions := ser.GetSidebarSuggestions(3)
				statuses := ser.GetHumanStatusBatch([]string{"writer"})
				if len(suggestions.RecentlyJoined) > 3 || len(suggestions.Trending) > 3 {
					t.Errorf("sidebar suggestions exceeded requested limit: %+v", suggestions)
					return
				}
				if _, ok := statuses["writer"]; !ok {
					t.Error("expected writer status to be present")
					return
				}
			}
		}()
	}

	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		for i := 0; i < 10; i++ {
			postID := fmt.Sprintf("writer-%d", i)
			ser.AddUserActivity("writer", ActivityObject{
				Username:      "writer",
				PostId:        postID,
				Id:            postID,
				UnixTimestamp: time.Now().Unix(),
			})
		}
	}()

	writers.Wait()
	readers.Wait()

	waitForDerivedState(t, ser, func(snapshot *derivedStateSnapshot) bool {
		return snapshot != nil && snapshot.RebuiltAt.UnixNano() > 0
	})
}

func BenchmarkDerivedStateReadPath(b *testing.B) {
	ser := newRecommendationTestService()
	now := time.Now().Unix()

	for i := 0; i < 2000; i++ {
		username := fmt.Sprintf("bot-%d", i)
		vouches := []string{}
		if i%3 == 0 {
			vouches = append(vouches, "popular")
		}
		ser.RegisteredBots[username] = &RegisteredBot{
			Username:     username,
			RegisteredAt: now - int64(i),
			VouchedHuman: vouches,
		}
		ser.LocalActivity = append(ser.LocalActivity, ActivityObject{
			Username:      username,
			PostId:        fmt.Sprintf("post-%d", i),
			Id:            fmt.Sprintf("post-%d", i),
			UnixTimestamp: now - int64(i%300),
			ReplyCount:    i % 5,
			LikedBy:       []string{"fan1", "fan2"},
		})
	}
	ser.buildDerivedState()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = ser.GetSidebarSuggestions(3)
		_ = ser.GetHumanStatusBatch([]string{"popular", "bot-1", "bot-2"})
	}
}

func waitForDerivedState(t *testing.T, ser *Service, check func(snapshot *derivedStateSnapshot) bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, _ := ser.loadDerivedStateSnapshot()
		if snapshot != nil && check(snapshot) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	snapshot, _ := ser.loadDerivedStateSnapshot()
	t.Fatalf("condition not met before timeout; latest snapshot: %+v", snapshot)
}

func containsSuggestion(suggestions []SidebarSuggestion, username string) bool {
	for _, suggestion := range suggestions {
		if suggestion.Username == username {
			return true
		}
	}

	return false
}
