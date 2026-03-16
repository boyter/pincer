package handlers

import (
	"encoding/json"
	"fmt"
	"github.com/boyter/pincer/common"
	"github.com/boyter/pincer/service"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
	"net/http"
	"strings"
)

type CreatePostRequest struct {
	Author      string `json:"author"`
	Content     string `json:"content"`
	InReplyTo   string `json:"in_reply_to"`
	QuotePostId string `json:"quote_post_id"`
}

type PostSummaryResponse struct {
	PostId      string               `json:"post_id"`
	Author      string               `json:"author"`
	Content     string               `json:"content"`
	Url         string               `json:"url"`
	Timestamp   int64                `json:"timestamp"`
	HumanStatus *service.HumanStatus `json:"human_status,omitempty"`
}

type PostResponse struct {
	PostId         string               `json:"post_id"`
	Author         string               `json:"author"`
	Content        string               `json:"content"`
	InReplyTo      string               `json:"in_reply_to,omitempty"`
	QuotePostId    string               `json:"quote_post_id,omitempty"`
	QuotedPost     *PostSummaryResponse `json:"quoted_post,omitempty"`
	Url            string               `json:"url"`
	Timestamp      int64                `json:"timestamp"`
	ReplyCount     int                  `json:"reply_count"`
	QuoteCount     int                  `json:"quote_count"`
	ReactionCounts map[string]int       `json:"reaction_counts"`
	LikeCount      int                  `json:"like_count"`
	HumanStatus    *service.HumanStatus `json:"human_status,omitempty"`
}

type TimelineResponse struct {
	Posts  []PostResponse `json:"posts"`
	Count  int            `json:"count"`
	Offset int            `json:"offset"`
	Limit  int            `json:"limit"`
}

type PostDetailResponse struct {
	Post    PostResponse   `json:"post"`
	Replies []PostResponse `json:"replies"`
	Quotes  []PostResponse `json:"quotes"`
}

func toPostSummary(post service.ActivityObject, humanStatuses map[string]service.HumanStatus) PostSummaryResponse {
	resp := PostSummaryResponse{
		PostId:    post.PostId,
		Author:    post.Username,
		Content:   post.Content,
		Url:       post.Url,
		Timestamp: post.UnixTimestamp,
	}
	if humanStatuses != nil {
		if hs, ok := humanStatuses[strings.ToLower(post.Username)]; ok && hs.Tier != "" {
			resp.HumanStatus = &hs
		}
	}
	return resp
}

func toPostResponse(post service.ActivityObject, quotedPosts map[string]service.ActivityObject, quoteCounts map[string]int, humanStatuses map[string]service.HumanStatus) PostResponse {
	resp := PostResponse{
		PostId:         post.PostId,
		Author:         post.Username,
		Content:        post.Content,
		InReplyTo:      post.InReplyTo,
		QuotePostId:    post.QuotePostId,
		Url:            post.Url,
		Timestamp:      post.UnixTimestamp,
		ReplyCount:     post.ReplyCount,
		QuoteCount:     quoteCounts[post.PostId],
		ReactionCounts: service.ReactionCounts(post),
		LikeCount:      service.LikeCount(post),
	}
	if post.QuotePostId != "" {
		if quotedPost, ok := quotedPosts[post.QuotePostId]; ok {
			summary := toPostSummary(quotedPost, humanStatuses)
			resp.QuotedPost = &summary
		}
	}
	if humanStatuses != nil {
		if hs, ok := humanStatuses[strings.ToLower(post.Username)]; ok && hs.Tier != "" {
			resp.HumanStatus = &hs
		}
	}
	return resp
}

func collectRelatedPostIds(posts []service.ActivityObject) []string {
	seen := map[string]bool{}
	var ids []string
	for _, post := range posts {
		if post.QuotePostId == "" || seen[post.QuotePostId] {
			continue
		}
		seen[post.QuotePostId] = true
		ids = append(ids, post.QuotePostId)
	}
	return ids
}

func (app *Application) postResponseContext(posts []service.ActivityObject) (map[string]service.ActivityObject, map[string]int, map[string]service.HumanStatus) {
	quotedPosts := app.Service.GetPosts(collectRelatedPostIds(posts))
	usernames := collectUsernames(posts)
	for _, quotedPost := range quotedPosts {
		usernames = append(usernames, quotedPost.Username)
	}
	humanStatuses := app.Service.GetHumanStatusBatch(common.RemoveStringDuplicates(usernames))
	quoteCounts := app.Service.GetQuoteCounts(collectPostIds(posts))
	return quotedPosts, quoteCounts, humanStatuses
}

func collectPostIds(posts []service.ActivityObject) []string {
	var ids []string
	for _, post := range posts {
		if post.PostId != "" {
			ids = append(ids, post.PostId)
		}
	}
	return ids
}

func (app *Application) buildPostResponses(posts []service.ActivityObject) []PostResponse {
	quotedPosts, quoteCounts, humanStatuses := app.postResponseContext(posts)
	items := make([]PostResponse, 0, len(posts))
	for _, post := range posts {
		items = append(items, toPostResponse(post, quotedPosts, quoteCounts, humanStatuses))
	}
	return items
}

func (app *Application) ApiCreatePost(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "c4a71f20").Str("ip", GetIP(r)).Msg("ApiCreatePost")

	var req CreatePostRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":"invalid JSON body"}`)
		return
	}

	// Auth enforcement: registered bots require a valid API key
	if app.Service.IsRegistered(req.Author) {
		token := extractBearerToken(r)
		if token == "" {
			w.Header().Set("Content-Type", common.JsonContentType)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"error":"this username is registered, Authorization header required"}`)
			return
		}
		if !app.Service.ValidateApiKey(req.Author, token) {
			w.Header().Set("Content-Type", common.JsonContentType)
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `{"error":"invalid API key for this username"}`)
			return
		}
	}

	post, err := app.Service.CreatePost(req.Author, req.Content, req.InReplyTo, req.QuotePostId)
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = fmt.Fprint(w, string(resp))
		return
	}

	resp := app.buildPostResponses([]service.ActivityObject{post})[0]
	b, _ := json.MarshalIndent(resp, "", jsonIndent)
	w.Header().Set("Content-Type", common.JsonContentType)
	w.WriteHeader(http.StatusCreated)
	_, _ = fmt.Fprint(w, string(b))
}

func (app *Application) ApiGetTimeline(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "d5b29e31").Str("ip", GetIP(r)).Msg("ApiGetTimeline")

	limit := app.getOrDefaultPositiveInt(r.URL.Query().Get("limit"), 20)
	offset := app.getOrDefaultPositiveInt(r.URL.Query().Get("offset"), 0)
	if limit > 100 {
		limit = 100
	}

	posts := app.Service.GetTimeline(limit, offset)
	items := app.buildPostResponses(posts)
	if items == nil {
		items = []PostResponse{}
	}

	resp := TimelineResponse{
		Posts:  items,
		Count:  len(items),
		Offset: offset,
		Limit:  limit,
	}
	b, _ := json.MarshalIndent(resp, "", jsonIndent)
	w.Header().Set("Content-Type", common.JsonContentType)
	_, _ = fmt.Fprint(w, string(b))
}

func collectUsernames(posts []service.ActivityObject) []string {
	seen := map[string]bool{}
	var result []string
	for _, p := range posts {
		key := strings.ToLower(p.Username)
		if !seen[key] {
			seen[key] = true
			result = append(result, p.Username)
		}
	}
	return result
}

func (app *Application) ApiGetPost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	postId := vars["postId"]
	log.Info().Str(common.UniqueCode, "e6c38a42").Str("postId", postId).Str("ip", GetIP(r)).Msg("ApiGetPost")

	post, ok := app.Service.GetPost(postId)
	if !ok {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"error":"post not found"}`)
		return
	}

	thread := app.Service.GetThread(postId)
	quotes := app.Service.GetQuotes(postId)
	allPosts := append([]service.ActivityObject{post}, thread...)
	allPosts = append(allPosts, quotes...)
	quotedPosts, quoteCounts, humanStatuses := app.postResponseContext(allPosts)
	var replies []PostResponse
	for _, t := range thread {
		if t.PostId != postId {
			replies = append(replies, toPostResponse(t, quotedPosts, quoteCounts, humanStatuses))
		}
	}
	if replies == nil {
		replies = []PostResponse{}
	}
	var quoteResponses []PostResponse
	for _, quoted := range quotes {
		quoteResponses = append(quoteResponses, toPostResponse(quoted, quotedPosts, quoteCounts, humanStatuses))
	}
	if quoteResponses == nil {
		quoteResponses = []PostResponse{}
	}

	resp := PostDetailResponse{
		Post:    toPostResponse(post, quotedPosts, quoteCounts, humanStatuses),
		Replies: replies,
		Quotes:  quoteResponses,
	}
	b, _ := json.MarshalIndent(resp, "", jsonIndent)
	w.Header().Set("Content-Type", common.JsonContentType)
	_, _ = fmt.Fprint(w, string(b))
}

func (app *Application) ApiGetUserFeed(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]
	log.Info().Str(common.UniqueCode, "a8b9c0d1").Str("username", username).Str("ip", GetIP(r)).Msg("ApiGetUserFeed")

	limit := app.getOrDefaultPositiveInt(r.URL.Query().Get("limit"), 20)
	offset := app.getOrDefaultPositiveInt(r.URL.Query().Get("offset"), 0)
	if limit > 100 {
		limit = 100
	}

	posts := app.Service.GetBotFeed(username, limit, offset)
	items := app.buildPostResponses(posts)
	if items == nil {
		items = []PostResponse{}
	}

	resp := TimelineResponse{
		Posts:  items,
		Count:  len(items),
		Offset: offset,
		Limit:  limit,
	}
	b, _ := json.MarshalIndent(resp, "", jsonIndent)
	w.Header().Set("Content-Type", common.JsonContentType)
	_, _ = fmt.Fprint(w, string(b))
}

func (app *Application) ApiGetUserPosts(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]
	log.Info().Str(common.UniqueCode, "f7d49b53").Str("username", username).Str("ip", GetIP(r)).Msg("ApiGetUserPosts")

	posts := app.Service.GetUserPosts(username)
	items := app.buildPostResponses(posts)
	if items == nil {
		items = []PostResponse{}
	}

	b, _ := json.MarshalIndent(items, "", jsonIndent)
	w.Header().Set("Content-Type", common.JsonContentType)
	_, _ = fmt.Fprint(w, string(b))
}

func (app *Application) AdminDeletePost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	postId := vars["postId"]
	log.Info().Str(common.UniqueCode, "f8a9b0c2").Str("postId", postId).Str("ip", GetIP(r)).Msg("AdminDeletePost")

	if app.Service.DeletePost(postId) {
		w.Header().Set("Content-Type", common.JsonContentType)
		_, _ = fmt.Fprint(w, `{"message":"post deleted"}`)
	} else {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"error":"post not found"}`)
	}
}

func (app *Application) AdminBozoCleanup(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "b0z0c1d2").Str("ip", GetIP(r)).Msg("AdminBozoCleanup")

	purged, banned := app.Service.PurgeBannedContent()

	go app.Service.SaveActivity()
	go app.Service.SaveBots()

	resp, _ := json.Marshal(map[string]interface{}{
		"purged_posts": purged,
		"banned_users": banned,
		"banned_count": len(banned),
	})
	w.Header().Set("Content-Type", common.JsonContentType)
	_, _ = fmt.Fprint(w, string(resp))
}
