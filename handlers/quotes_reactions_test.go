package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/boyter/pincer/common"
	"github.com/boyter/pincer/service"
	"github.com/gorilla/mux"
)

func newHandlerTestApp(t *testing.T) (*Application, *service.Service) {
	t.Helper()

	dir := t.TempDir()
	env := &common.Environment{
		BaseUrl:          "http://localhost:8001/",
		SiteName:         "Pincer",
		MaxPostLength:    500,
		ActivityFilePath: filepath.Join(dir, "activity.json"),
		BotsFilePath:     filepath.Join(dir, "bots.json"),
	}

	ser, err := service.NewService(env)
	if err != nil {
		t.Fatalf("unexpected service error: %v", err)
	}
	app, err := NewApplication(env, ser)
	if err != nil {
		t.Fatalf("unexpected application error: %v", err)
	}

	return &app, ser
}

func waitForHandlerPersistence() {
	time.Sleep(25 * time.Millisecond)
}

func TestApiGetPostIncludesQuotesAndReactionSummary(t *testing.T) {
	app, ser := newHandlerTestApp(t)

	source, err := ser.CreatePost("sourcebot", "source pinch", "", "")
	if err != nil {
		t.Fatalf("unexpected source create error: %v", err)
	}
	if err := ser.AddReaction(source.PostId, "fanbot", "like"); err != nil {
		t.Fatalf("unexpected like error: %v", err)
	}
	quoted, err := ser.CreatePost("quotebot", "quote pinch", "", source.PostId)
	if err != nil {
		t.Fatalf("unexpected quote create error: %v", err)
	}
	if err := ser.AddReaction(quoted.PostId, "fanbot", "boost"); err != nil {
		t.Fatalf("unexpected boost error: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/posts/"+source.PostId, nil)
	req = mux.SetURLVars(req, map[string]string{"postId": source.PostId})
	rec := httptest.NewRecorder()

	app.ApiGetPost(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp PostDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unexpected json error: %v", err)
	}

	if resp.Post.LikeCount != 1 {
		t.Fatalf("expected legacy like_count 1, got %d", resp.Post.LikeCount)
	}
	if resp.Post.ReactionCounts["like"] != 1 {
		t.Fatalf("expected reaction_counts.like 1, got %#v", resp.Post.ReactionCounts)
	}
	if resp.Post.QuoteCount != 1 {
		t.Fatalf("expected quote_count 1, got %d", resp.Post.QuoteCount)
	}
	if len(resp.Quotes) != 1 {
		t.Fatalf("expected one quote entry, got %d", len(resp.Quotes))
	}
	if resp.Quotes[0].QuotePostId != source.PostId {
		t.Fatalf("expected quote_post_id %s, got %s", source.PostId, resp.Quotes[0].QuotePostId)
	}
	if resp.Quotes[0].QuotedPost == nil || resp.Quotes[0].QuotedPost.PostId != source.PostId {
		t.Fatalf("expected embedded quoted_post summary, got %#v", resp.Quotes[0].QuotedPost)
	}
	waitForHandlerPersistence()
}

func TestTimelineAndPostTemplatesRenderQuotePreviewAndReactions(t *testing.T) {
	app, ser := newHandlerTestApp(t)

	source, err := ser.CreatePost("sourcebot", "source pinch", "", "")
	if err != nil {
		t.Fatalf("unexpected source create error: %v", err)
	}
	quoted, err := ser.CreatePost("quotebot", "quote pinch", "", source.PostId)
	if err != nil {
		t.Fatalf("unexpected quote create error: %v", err)
	}
	if err := ser.AddReaction(quoted.PostId, "fanbot", "boost"); err != nil {
		t.Fatalf("unexpected boost error: %v", err)
	}

	timelineReq := httptest.NewRequest("GET", "/_/feed", nil)
	timelineRec := httptest.NewRecorder()
	app.TimelinePartial(timelineRec, timelineReq)
	body := timelineRec.Body.String()
	if timelineRec.Code != 200 {
		t.Fatalf("expected 200 from timeline partial, got %d", timelineRec.Code)
	}
	if !strings.Contains(body, "quote-preview") {
		t.Fatalf("expected quote preview markup, got %s", body)
	}
	if !strings.Contains(body, "Boost 1") {
		t.Fatalf("expected boost reaction summary, got %s", body)
	}

	postReq := httptest.NewRequest("GET", "/post/"+source.PostId+"/", nil)
	postReq = mux.SetURLVars(postReq, map[string]string{"postId": source.PostId})
	postRec := httptest.NewRecorder()
	app.PostView(postRec, postReq)
	postBody := postRec.Body.String()
	if postRec.Code != 200 {
		t.Fatalf("expected 200 from post view, got %d", postRec.Code)
	}
	if !strings.Contains(postBody, "Quotes") {
		t.Fatalf("expected quotes section, got %s", postBody)
	}
	if !strings.Contains(postBody, quoted.PostId) {
		t.Fatalf("expected quoted post id in rendered page, got %s", postBody)
	}
	waitForHandlerPersistence()
}
