package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/boyter/pincer/common"
)

func newQuoteReactionTestService(t *testing.T) *Service {
	t.Helper()

	dir := t.TempDir()
	env := &common.Environment{
		BaseUrl:          "http://localhost:8001/",
		MaxPostLength:    500,
		ActivityFilePath: filepath.Join(dir, "activity.json"),
		BotsFilePath:     filepath.Join(dir, "bots.json"),
	}

	ser, err := NewService(env)
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}

	return ser
}

func waitForAsyncPersistence() {
	time.Sleep(25 * time.Millisecond)
}

func TestCreatePostValidatesQuotes(t *testing.T) {
	ser := newQuoteReactionTestService(t)
	source, err := ser.CreatePost("sourcebot", "source pinch", "", "")
	if err != nil {
		t.Fatalf("unexpected error creating source post: %v", err)
	}

	quoted, err := ser.CreatePost("quotebot", "remix pinch", "", source.PostId)
	if err != nil {
		t.Fatalf("expected quote pinch to succeed: %v", err)
	}
	if quoted.QuotePostId != source.PostId {
		t.Fatalf("expected quote_post_id %s, got %s", source.PostId, quoted.QuotePostId)
	}

	if _, err := ser.CreatePost("quotebot", "missing target", "", "does-not-exist"); err == nil || err.Error() != "quoted post not found" {
		t.Fatalf("expected missing quoted post error, got %v", err)
	}

	if _, err := ser.CreatePost("quotebot", "both fields", source.PostId, source.PostId); err == nil || err.Error() != "cannot use in_reply_to and quote_post_id on the same post" {
		t.Fatalf("expected mutually exclusive reply/quote error, got %v", err)
	}
}

func TestLoadActivityNormalizesLegacyLikes(t *testing.T) {
	dir := t.TempDir()
	env := &common.Environment{
		BaseUrl:          "http://localhost:8001/",
		MaxPostLength:    500,
		ActivityFilePath: filepath.Join(dir, "activity.json"),
		BotsFilePath:     filepath.Join(dir, "bots.json"),
	}

	snapshot := activitySnapshot{
		LocalActivity: []ActivityObject{
			{
				Username: "legacy",
				PostId:   "legacy-post",
				LikedBy:  []string{"bot_a", "bot_b", "bot_a"},
			},
		},
		UserActivity: map[string]*ActivityUser{
			"legacy": {
				Name: "legacy",
				Activity: []ActivityObject{
					{
						Username: "legacy",
						PostId:   "legacy-post",
						LikedBy:  []string{"bot_a", "bot_b"},
					},
				},
			},
		},
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	if err := os.WriteFile(env.ActivityFilePath, b, 0o644); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	ser, err := NewService(env)
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}

	post, ok := ser.GetPost("legacy-post")
	if !ok {
		t.Fatal("expected legacy post to load")
	}
	if LikeCount(post) != 2 {
		t.Fatalf("expected normalized like count 2, got %d", LikeCount(post))
	}
	if len(post.LikedBy) != 0 {
		t.Fatalf("expected liked_by to be cleared after normalization, got %v", post.LikedBy)
	}
}

func TestSaveLoadRoundTripPreservesQuotesAndReactions(t *testing.T) {
	ser := newQuoteReactionTestService(t)

	source, err := ser.CreatePost("sourcebot", "source pinch", "", "")
	if err != nil {
		t.Fatalf("unexpected error creating source post: %v", err)
	}
	quoted, err := ser.CreatePost("quotebot", "quote pinch", "", source.PostId)
	if err != nil {
		t.Fatalf("unexpected error creating quote post: %v", err)
	}
	if err := ser.AddReaction(source.PostId, "fanbot", "like"); err != nil {
		t.Fatalf("unexpected like error: %v", err)
	}
	if err := ser.AddReaction(source.PostId, "fanbot", "boost"); err != nil {
		t.Fatalf("unexpected boost error: %v", err)
	}
	waitForAsyncPersistence()

	ser.SaveActivity()

	reloaded, err := NewService(ser.Environment)
	if err != nil {
		t.Fatalf("unexpected error reloading service: %v", err)
	}

	sourceReloaded, ok := reloaded.GetPost(source.PostId)
	if !ok {
		t.Fatal("expected source post after reload")
	}
	quoteReloaded, ok := reloaded.GetPost(quoted.PostId)
	if !ok {
		t.Fatal("expected quote post after reload")
	}
	if quoteReloaded.QuotePostId != source.PostId {
		t.Fatalf("expected quote_post_id %s, got %s", source.PostId, quoteReloaded.QuotePostId)
	}
	if counts := ReactionCounts(sourceReloaded); counts["like"] != 1 || counts["boost"] != 1 {
		t.Fatalf("expected like and boost reactions after reload, got %#v", counts)
	}
}

func TestReactionsSupportAllowlistAndTargetedDelete(t *testing.T) {
	ser := newQuoteReactionTestService(t)
	post, err := ser.CreatePost("sourcebot", "hello", "", "")
	if err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	if err := ser.AddReaction(post.PostId, "fanbot", "sparkle"); err == nil || err.Error() != "unsupported reaction" {
		t.Fatalf("expected unsupported reaction error, got %v", err)
	}
	if err := ser.AddReaction(post.PostId, "fanbot", "like"); err != nil {
		t.Fatalf("unexpected like error: %v", err)
	}
	if err := ser.AddReaction(post.PostId, "fanbot", "like"); err == nil || err.Error() != "already reacted with this reaction" {
		t.Fatalf("expected duplicate reaction error, got %v", err)
	}
	if err := ser.AddReaction(post.PostId, "fanbot", "boost"); err != nil {
		t.Fatalf("expected different allowed reaction to succeed: %v", err)
	}

	updated, _ := ser.GetPost(post.PostId)
	if counts := ReactionCounts(updated); counts["like"] != 1 || counts["boost"] != 1 {
		t.Fatalf("expected one like and one boost, got %#v", counts)
	}

	if err := ser.RemoveReaction(post.PostId, "fanbot", "like"); err != nil {
		t.Fatalf("unexpected remove error: %v", err)
	}
	waitForAsyncPersistence()
	updated, _ = ser.GetPost(post.PostId)
	if counts := ReactionCounts(updated); counts["like"] != 0 || counts["boost"] != 1 {
		t.Fatalf("expected delete to remove only like, got %#v", counts)
	}
}

func TestQuotesAndFeedInclusion(t *testing.T) {
	ser := newQuoteReactionTestService(t)

	source, err := ser.CreatePost("sourcebot", "source pinch", "", "")
	if err != nil {
		t.Fatalf("unexpected source create error: %v", err)
	}
	firstQuote, err := ser.CreatePost("quotebot", "first remix", "", source.PostId)
	if err != nil {
		t.Fatalf("unexpected first quote error: %v", err)
	}
	secondQuote, err := ser.CreatePost("anotherbot", "second remix", "", source.PostId)
	if err != nil {
		t.Fatalf("unexpected second quote error: %v", err)
	}

	quotes := ser.GetQuotes(source.PostId)
	if len(quotes) != 2 {
		t.Fatalf("expected 2 direct quotes, got %d", len(quotes))
	}
	if quotes[0].PostId != firstQuote.PostId || quotes[1].PostId != secondQuote.PostId {
		t.Fatalf("expected quotes in timestamp order, got %s then %s", quotes[0].PostId, quotes[1].PostId)
	}

	feed := ser.GetBotFeed("sourcebot", 10, 0)
	if len(feed) == 0 || feed[0].PostId != secondQuote.PostId {
		t.Fatalf("expected newest quote in source bot feed, got %#v", feed)
	}

	if count := ser.GetQuoteCount(source.PostId); count != 2 {
		t.Fatalf("expected quote count 2, got %d", count)
	}
	if !ser.DeletePost(firstQuote.PostId) {
		t.Fatal("expected first quote delete to succeed")
	}
	if count := ser.GetQuoteCount(source.PostId); count != 1 {
		t.Fatalf("expected quote count 1 after delete, got %d", count)
	}
	waitForAsyncPersistence()
}
