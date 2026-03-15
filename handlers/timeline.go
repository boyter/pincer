package handlers

import (
	"github.com/boyter/pincer/common"
	"github.com/boyter/pincer/service"
	"github.com/gorilla/mux"
	"github.com/microcosm-cc/bluemonday"
	"github.com/rs/zerolog/log"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var htmlTagRegexp = regexp.MustCompile(`<[^>]*>`)

type TimelinePost struct {
	Username        string
	PostId          string
	Content         template.HTML
	ContentText     string
	TimeAgo         string
	ReplyCount      int
	LikeCount       int
	InReplyTo       string
	AvatarUrl       string
	IsLocal         bool
	ReplyToUsername string
	HumanTier       string
	HumanTierClass  string
}

type SidebarProfile struct {
	Username       string
	AvatarUrl      string
	Label          string
	HumanTier      string
	HumanTierClass string
}

type TimelineData struct {
	SiteName        string
	Posts           []TimelinePost
	BaseUrl         string
	ActiveNav       string
	ShowSidebarInfo bool
	RecentlyJoined  []SidebarProfile
	Trending        []SidebarProfile
}

type PostViewData struct {
	SiteName        string
	Post            TimelinePost
	Parent          *TimelinePost
	Replies         []TimelinePost
	BaseUrl         string
	ActiveNav       string
	ShowSidebarInfo bool
	RecentlyJoined  []SidebarProfile
	Trending        []SidebarProfile
}

type ProfileData struct {
	SiteName        string
	Username        string
	Posts           []TimelinePost
	Feed            []TimelinePost
	BaseUrl         string
	HumanTier       string
	HumanTierClass  string
	ActiveNav       string
	ShowSidebarInfo bool
	RecentlyJoined  []SidebarProfile
	Trending        []SidebarProfile
}

type SearchData struct {
	SiteName        string
	Query           string
	Posts           []TimelinePost
	BaseUrl         string
	ActiveNav       string
	ShowSidebarInfo bool
	RecentlyJoined  []SidebarProfile
	Trending        []SidebarProfile
}

type DashboardData struct {
	SiteName        string
	Stats           service.DashboardStats
	BaseUrl         string
	ActiveNav       string
	ShowSidebarInfo bool
	RecentlyJoined  []SidebarProfile
	Trending        []SidebarProfile
}

type DocsData struct {
	SiteName        string
	BaseUrl         string
	MaxPostLength   int
	ActiveNav       string
	ShowSidebarInfo bool
	RecentlyJoined  []SidebarProfile
	Trending        []SidebarProfile
}

func activityToTimelinePost(username, postId, content string, unixTimestamp int64, replyCount, likeCount int, inReplyTo string, isLocal bool, baseUrl string, sanitizer *bluemonday.Policy, humanStatuses map[string]service.HumanStatus) TimelinePost {
	cleanContent := sanitizer.Sanitize(content)
	plainText := htmlTagRegexp.ReplaceAllString(cleanContent, "")
	if len(plainText) > 200 {
		plainText = plainText[:200] + "..."
	}
	tp := TimelinePost{
		Username:    username,
		PostId:      postId,
		Content:     template.HTML(cleanContent),
		ContentText: plainText,
		TimeAgo:     common.FromTime(time.Unix(unixTimestamp, 0)),
		ReplyCount:  replyCount,
		LikeCount:   likeCount,
		InReplyTo:   inReplyTo,
		AvatarUrl:   "/u/" + username + "/image",
		IsLocal:     isLocal,
	}
	if humanStatuses != nil {
		if hs, ok := humanStatuses[strings.ToLower(username)]; ok {
			tp.HumanTier = hs.Tier
			tp.HumanTierClass = hs.TierClass
		}
	}
	return tp
}

func (app *Application) resolveReplyUsernames(posts []TimelinePost) {
	for i, p := range posts {
		if p.InReplyTo != "" {
			parent, ok := app.Service.GetPost(p.InReplyTo)
			if ok {
				posts[i].ReplyToUsername = parent.Username
			}
		}
	}
}

func (app *Application) Timeline(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "a1b2c3d4").Str("ip", GetIP(r)).Msg("Timeline")

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	limit := app.getOrDefaultPositiveInt(r.URL.Query().Get("limit"), 100)
	offset := app.getOrDefaultPositiveInt(r.URL.Query().Get("offset"), 0)
	if limit > 100 {
		limit = 100
	}

	posts := app.Service.GetTimeline(limit, offset)
	sanitizer := bluemonday.UGCPolicy()
	usernames := collectUsernames(posts)
	humanStatuses := app.Service.GetHumanStatusBatch(usernames)

	var timelinePosts []TimelinePost
	for _, p := range posts {
		timelinePosts = append(timelinePosts, activityToTimelinePost(
			p.Username, p.PostId, p.Content, p.UnixTimestamp, p.ReplyCount, len(p.LikedBy), p.InReplyTo, p.IsLocal, app.Environment.BaseUrl, sanitizer, humanStatuses,
		))
	}

	app.resolveReplyUsernames(timelinePosts)
	recentlyJoined, trending := app.sidebarProfiles()

	err := timelineTemplate.ExecuteTemplate(w, "timeline.tmpl", TimelineData{
		SiteName:        app.Environment.SiteName,
		Posts:           timelinePosts,
		BaseUrl:         app.Environment.BaseUrl,
		ActiveNav:       "home",
		ShowSidebarInfo: true,
		RecentlyJoined:  recentlyJoined,
		Trending:        trending,
	})
	if err != nil {
		log.Error().Str(common.UniqueCode, "e4f5a6b7").Str("ip", GetIP(r)).Err(err).Msg("error executing timeline template")
		http.Error(w, "Internal Server Error", 500)
	}
}

func (app *Application) TimelinePartial(w http.ResponseWriter, r *http.Request) {
	limit := app.getOrDefaultPositiveInt(r.URL.Query().Get("limit"), 100)
	offset := app.getOrDefaultPositiveInt(r.URL.Query().Get("offset"), 0)
	if limit > 100 {
		limit = 100
	}

	posts := app.Service.GetTimeline(limit, offset)
	sanitizer := bluemonday.UGCPolicy()
	usernames := collectUsernames(posts)
	humanStatuses := app.Service.GetHumanStatusBatch(usernames)

	var timelinePosts []TimelinePost
	for _, p := range posts {
		timelinePosts = append(timelinePosts, activityToTimelinePost(
			p.Username, p.PostId, p.Content, p.UnixTimestamp, p.ReplyCount, len(p.LikedBy), p.InReplyTo, p.IsLocal, app.Environment.BaseUrl, sanitizer, humanStatuses,
		))
	}

	app.resolveReplyUsernames(timelinePosts)

	err := feedPartialTemplate.ExecuteTemplate(w, "feed_partial.tmpl", TimelineData{
		SiteName: app.Environment.SiteName,
		Posts:    timelinePosts,
		BaseUrl:  app.Environment.BaseUrl,
	})
	if err != nil {
		log.Error().Str(common.UniqueCode, "d1e2f3a4").Str("ip", GetIP(r)).Err(err).Msg("error executing feed partial template")
		http.Error(w, "Internal Server Error", 500)
	}
}

func (app *Application) PostView(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	postId := vars["postId"]
	log.Info().Str(common.UniqueCode, "b2c3d4e5").Str("postId", postId).Str("ip", GetIP(r)).Msg("PostView")

	post, ok := app.Service.GetPost(postId)
	if !ok {
		http.NotFound(w, r)
		return
	}

	sanitizer := bluemonday.UGCPolicy()
	thread := app.Service.GetThread(postId)

	// Collect all usernames for human status batch lookup
	allPosts := append([]service.ActivityObject{post}, thread...)
	if post.InReplyTo != "" {
		if parentPost, parentOk := app.Service.GetPost(post.InReplyTo); parentOk {
			allPosts = append(allPosts, parentPost)
		}
	}
	usernames := collectUsernames(allPosts)
	humanStatuses := app.Service.GetHumanStatusBatch(usernames)

	mainPost := activityToTimelinePost(
		post.Username, post.PostId, post.Content, post.UnixTimestamp, post.ReplyCount, len(post.LikedBy), post.InReplyTo, post.IsLocal, app.Environment.BaseUrl, sanitizer, humanStatuses,
	)

	var parent *TimelinePost
	if post.InReplyTo != "" {
		parentPost, parentOk := app.Service.GetPost(post.InReplyTo)
		if parentOk {
			p := activityToTimelinePost(
				parentPost.Username, parentPost.PostId, parentPost.Content, parentPost.UnixTimestamp, parentPost.ReplyCount, len(parentPost.LikedBy), parentPost.InReplyTo, parentPost.IsLocal, app.Environment.BaseUrl, sanitizer, humanStatuses,
			)
			parent = &p
		}
	}

	var replies []TimelinePost
	for _, t := range thread {
		if t.PostId != postId {
			replies = append(replies, activityToTimelinePost(
				t.Username, t.PostId, t.Content, t.UnixTimestamp, t.ReplyCount, len(t.LikedBy), t.InReplyTo, t.IsLocal, app.Environment.BaseUrl, sanitizer, humanStatuses,
			))
		}
	}

	mainPosts := []TimelinePost{mainPost}
	app.resolveReplyUsernames(mainPosts)
	mainPost = mainPosts[0]
	app.resolveReplyUsernames(replies)
	recentlyJoined, trending := app.sidebarProfiles()

	err := postTemplate.ExecuteTemplate(w, "post.tmpl", PostViewData{
		SiteName:       app.Environment.SiteName,
		Post:           mainPost,
		Parent:         parent,
		Replies:        replies,
		BaseUrl:        app.Environment.BaseUrl,
		ActiveNav:      "home",
		RecentlyJoined: recentlyJoined,
		Trending:       trending,
	})
	if err != nil {
		log.Error().Str(common.UniqueCode, "c3d4e5f6").Str("ip", GetIP(r)).Err(err).Msg("error executing post template")
		http.Error(w, "Internal Server Error", 500)
	}
}

func (app *Application) Profile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]
	log.Info().Str(common.UniqueCode, "d4e5f6a7").Str("username", username).Str("ip", GetIP(r)).Msg("Profile")

	posts := app.Service.GetUserPosts(username)
	feed := app.Service.GetBotFeed(username, 50, 0)
	sanitizer := bluemonday.UGCPolicy()

	// Collect all usernames including the profile user
	allActivity := append(posts, feed...)
	usernames := collectUsernames(allActivity)
	// Ensure profile user is included
	profileKey := strings.ToLower(username)
	found := false
	for _, u := range usernames {
		if strings.ToLower(u) == profileKey {
			found = true
			break
		}
	}
	if !found {
		usernames = append(usernames, username)
	}
	humanStatuses := app.Service.GetHumanStatusBatch(usernames)

	var timelinePosts []TimelinePost
	for _, p := range posts {
		timelinePosts = append(timelinePosts, activityToTimelinePost(
			p.Username, p.PostId, p.Content, p.UnixTimestamp, p.ReplyCount, len(p.LikedBy), p.InReplyTo, p.IsLocal, app.Environment.BaseUrl, sanitizer, humanStatuses,
		))
	}

	var feedPosts []TimelinePost
	for _, p := range feed {
		feedPosts = append(feedPosts, activityToTimelinePost(
			p.Username, p.PostId, p.Content, p.UnixTimestamp, p.ReplyCount, len(p.LikedBy), p.InReplyTo, p.IsLocal, app.Environment.BaseUrl, sanitizer, humanStatuses,
		))
	}

	app.resolveReplyUsernames(timelinePosts)
	app.resolveReplyUsernames(feedPosts)

	profileHumanTier := ""
	profileHumanTierClass := ""
	if hs, ok := humanStatuses[profileKey]; ok {
		profileHumanTier = hs.Tier
		profileHumanTierClass = hs.TierClass
	}
	recentlyJoined, trending := app.sidebarProfiles()

	err := profileTemplate.ExecuteTemplate(w, "profile.tmpl", ProfileData{
		SiteName:       app.Environment.SiteName,
		Username:       username,
		Posts:          timelinePosts,
		Feed:           feedPosts,
		BaseUrl:        app.Environment.BaseUrl,
		HumanTier:      profileHumanTier,
		HumanTierClass: profileHumanTierClass,
		ActiveNav:      "home",
		RecentlyJoined: recentlyJoined,
		Trending:       trending,
	})
	if err != nil {
		log.Error().Str(common.UniqueCode, "e5f6a7b8").Str("ip", GetIP(r)).Err(err).Msg("error executing profile template")
		http.Error(w, "Internal Server Error", 500)
	}
}

func (app *Application) Search(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "a3e36433").Str("ip", GetIP(r)).Msg("Search")

	q := r.URL.Query().Get("q")
	limit := app.getOrDefaultPositiveInt(r.URL.Query().Get("limit"), 50)
	offset := app.getOrDefaultPositiveInt(r.URL.Query().Get("offset"), 0)
	if limit > 100 {
		limit = 100
	}

	var timelinePosts []TimelinePost
	if q != "" {
		app.Service.IncrementSearchCount()
		posts := app.Service.SearchPosts(q, limit, offset)
		sanitizer := bluemonday.UGCPolicy()
		usernames := collectUsernames(posts)
		humanStatuses := app.Service.GetHumanStatusBatch(usernames)
		for _, p := range posts {
			timelinePosts = append(timelinePosts, activityToTimelinePost(
				p.Username, p.PostId, p.Content, p.UnixTimestamp, p.ReplyCount, len(p.LikedBy), p.InReplyTo, p.IsLocal, app.Environment.BaseUrl, sanitizer, humanStatuses,
			))
		}
	}

	app.resolveReplyUsernames(timelinePosts)
	recentlyJoined, trending := app.sidebarProfiles()

	err := searchTemplate.ExecuteTemplate(w, "search.tmpl", SearchData{
		SiteName:       app.Environment.SiteName,
		Query:          q,
		Posts:          timelinePosts,
		BaseUrl:        app.Environment.BaseUrl,
		ActiveNav:      "search",
		RecentlyJoined: recentlyJoined,
		Trending:       trending,
	})
	if err != nil {
		log.Error().Str(common.UniqueCode, "3f717776").Str("ip", GetIP(r)).Err(err).Msg("error executing search template")
		http.Error(w, "Internal Server Error", 500)
	}
}

func (app *Application) Docs(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "f6a7b8c9").Str("ip", GetIP(r)).Msg("Docs")
	recentlyJoined, trending := app.sidebarProfiles()

	err := docsTemplate.ExecuteTemplate(w, "docs.tmpl", DocsData{
		SiteName:       app.Environment.SiteName,
		BaseUrl:        app.Environment.BaseUrl,
		MaxPostLength:  app.Environment.MaxPostLength,
		ActiveNav:      "about",
		RecentlyJoined: recentlyJoined,
		Trending:       trending,
	})
	if err != nil {
		log.Error().Str(common.UniqueCode, "a7b8c9d0").Str("ip", GetIP(r)).Err(err).Msg("error executing docs template")
		http.Error(w, "Internal Server Error", 500)
	}
}

func (app *Application) Dashboard(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "b9c0d1e3").Str("ip", GetIP(r)).Msg("Dashboard")

	stats := app.Service.GetDashboardStats()
	recentlyJoined, trending := app.sidebarProfiles()

	err := dashboardTemplate.ExecuteTemplate(w, "dashboard.tmpl", DashboardData{
		SiteName:       app.Environment.SiteName,
		Stats:          stats,
		BaseUrl:        app.Environment.BaseUrl,
		RecentlyJoined: recentlyJoined,
		Trending:       trending,
	})
	if err != nil {
		log.Error().Str(common.UniqueCode, "c0d1e2f4").Str("ip", GetIP(r)).Err(err).Msg("error executing dashboard template")
		http.Error(w, "Internal Server Error", 500)
	}
}
