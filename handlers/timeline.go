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

type ReactionBadge struct {
	Slug     string
	Label    string
	Count    int
	CssClass string
}

type QuotedTimelinePost struct {
	Username       string
	PostId         string
	Content        template.HTML
	ContentText    string
	TimeAgo        string
	AvatarUrl      string
	HumanTier      string
	HumanTierClass string
}

type TimelinePost struct {
	Username        string
	PostId          string
	Content         template.HTML
	ContentText     string
	TimeAgo         string
	ReplyCount      int
	LikeCount       int
	QuoteCount      int
	InReplyTo       string
	QuotePostId     string
	QuotedPost      *QuotedTimelinePost
	ReactionCounts  map[string]int
	ReactionBadges  []ReactionBadge
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
	Quotes          []TimelinePost
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
	SiteName         string
	BaseUrl          string
	MaxPostLength    int
	AllowedReactions []string
	ActiveNav        string
	ShowSidebarInfo  bool
	RecentlyJoined   []SidebarProfile
	Trending         []SidebarProfile
}

var reactionBadgeConfig = []ReactionBadge{
	{Slug: "like", Label: "Like", CssClass: "reaction-like"},
	{Slug: "boost", Label: "Boost", CssClass: "reaction-boost"},
	{Slug: "laugh", Label: "Laugh", CssClass: "reaction-laugh"},
	{Slug: "hmm", Label: "Hmm", CssClass: "reaction-hmm"},
}

func summarizeTimelineContent(content string, sanitizer *bluemonday.Policy) (template.HTML, string) {
	cleanContent := sanitizer.Sanitize(content)
	plainText := htmlTagRegexp.ReplaceAllString(cleanContent, "")
	if len(plainText) > 200 {
		plainText = plainText[:200] + "..."
	}
	return template.HTML(cleanContent), plainText
}

func timelineHumanStatus(username string, humanStatuses map[string]service.HumanStatus) (string, string) {
	if humanStatuses == nil {
		return "", ""
	}
	if hs, ok := humanStatuses[strings.ToLower(username)]; ok {
		return hs.Tier, hs.TierClass
	}
	return "", ""
}

func buildReactionBadges(counts map[string]int) []ReactionBadge {
	var badges []ReactionBadge
	for _, config := range reactionBadgeConfig {
		if counts[config.Slug] == 0 {
			continue
		}
		badges = append(badges, ReactionBadge{
			Slug:     config.Slug,
			Label:    config.Label,
			Count:    counts[config.Slug],
			CssClass: config.CssClass,
		})
	}
	return badges
}

func activityToQuotedTimelinePost(activity service.ActivityObject, sanitizer *bluemonday.Policy, humanStatuses map[string]service.HumanStatus) *QuotedTimelinePost {
	if activity.PostId == "" {
		return nil
	}
	content, plainText := summarizeTimelineContent(activity.Content, sanitizer)
	humanTier, humanTierClass := timelineHumanStatus(activity.Username, humanStatuses)
	return &QuotedTimelinePost{
		Username:       activity.Username,
		PostId:         activity.PostId,
		Content:        content,
		ContentText:    plainText,
		TimeAgo:        common.FromTime(time.Unix(activity.UnixTimestamp, 0)),
		AvatarUrl:      "/u/" + activity.Username + "/image",
		HumanTier:      humanTier,
		HumanTierClass: humanTierClass,
	}
}

func activityToTimelinePost(activity service.ActivityObject, quotedPosts map[string]service.ActivityObject, quoteCounts map[string]int, sanitizer *bluemonday.Policy, humanStatuses map[string]service.HumanStatus) TimelinePost {
	content, plainText := summarizeTimelineContent(activity.Content, sanitizer)
	reactionCounts := service.ReactionCounts(activity)
	humanTier, humanTierClass := timelineHumanStatus(activity.Username, humanStatuses)
	tp := TimelinePost{
		Username:       activity.Username,
		PostId:         activity.PostId,
		Content:        content,
		ContentText:    plainText,
		TimeAgo:        common.FromTime(time.Unix(activity.UnixTimestamp, 0)),
		ReplyCount:     activity.ReplyCount,
		LikeCount:      service.LikeCount(activity),
		QuoteCount:     quoteCounts[activity.PostId],
		InReplyTo:      activity.InReplyTo,
		QuotePostId:    activity.QuotePostId,
		ReactionCounts: reactionCounts,
		ReactionBadges: buildReactionBadges(reactionCounts),
		AvatarUrl:      "/u/" + activity.Username + "/image",
		IsLocal:        activity.IsLocal,
		HumanTier:      humanTier,
		HumanTierClass: humanTierClass,
	}
	if activity.QuotePostId != "" {
		if quotedPost, ok := quotedPosts[activity.QuotePostId]; ok {
			tp.QuotedPost = activityToQuotedTimelinePost(quotedPost, sanitizer, humanStatuses)
		}
	}
	return tp
}

func (app *Application) buildTimelinePosts(posts []service.ActivityObject, sanitizer *bluemonday.Policy, humanStatuses map[string]service.HumanStatus, quotedPosts map[string]service.ActivityObject, quoteCounts map[string]int) []TimelinePost {
	timelinePosts := make([]TimelinePost, 0, len(posts))
	for _, post := range posts {
		timelinePosts = append(timelinePosts, activityToTimelinePost(post, quotedPosts, quoteCounts, sanitizer, humanStatuses))
	}
	app.resolveReplyUsernames(timelinePosts)
	return timelinePosts
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
	quotedPosts, quoteCounts, humanStatuses := app.postResponseContext(posts)
	timelinePosts := app.buildTimelinePosts(posts, sanitizer, humanStatuses, quotedPosts, quoteCounts)
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
	quotedPosts, quoteCounts, humanStatuses := app.postResponseContext(posts)
	timelinePosts := app.buildTimelinePosts(posts, sanitizer, humanStatuses, quotedPosts, quoteCounts)

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
	quotes := app.Service.GetQuotes(postId)

	// Collect all usernames for human status batch lookup
	allPosts := append([]service.ActivityObject{post}, thread...)
	allPosts = append(allPosts, quotes...)
	if post.InReplyTo != "" {
		if parentPost, parentOk := app.Service.GetPost(post.InReplyTo); parentOk {
			allPosts = append(allPosts, parentPost)
		}
	}
	quotedPosts, quoteCounts, humanStatuses := app.postResponseContext(allPosts)
	mainPost := activityToTimelinePost(post, quotedPosts, quoteCounts, sanitizer, humanStatuses)

	var parent *TimelinePost
	if post.InReplyTo != "" {
		parentPost, parentOk := app.Service.GetPost(post.InReplyTo)
		if parentOk {
			p := activityToTimelinePost(parentPost, quotedPosts, quoteCounts, sanitizer, humanStatuses)
			parent = &p
		}
	}

	var replies []TimelinePost
	for _, t := range thread {
		if t.PostId != postId {
			replies = append(replies, activityToTimelinePost(t, quotedPosts, quoteCounts, sanitizer, humanStatuses))
		}
	}
	var remixPosts []TimelinePost
	for _, quoted := range quotes {
		remixPosts = append(remixPosts, activityToTimelinePost(quoted, quotedPosts, quoteCounts, sanitizer, humanStatuses))
	}

	mainPosts := []TimelinePost{mainPost}
	app.resolveReplyUsernames(mainPosts)
	mainPost = mainPosts[0]
	if parent != nil {
		parentPosts := []TimelinePost{*parent}
		app.resolveReplyUsernames(parentPosts)
		*parent = parentPosts[0]
	}
	app.resolveReplyUsernames(replies)
	app.resolveReplyUsernames(remixPosts)
	recentlyJoined, trending := app.sidebarProfiles()

	err := postTemplate.ExecuteTemplate(w, "post.tmpl", PostViewData{
		SiteName:       app.Environment.SiteName,
		Post:           mainPost,
		Parent:         parent,
		Replies:        replies,
		Quotes:         remixPosts,
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

	allActivity := append(posts, feed...)
	quotedPosts := app.Service.GetPosts(collectRelatedPostIds(allActivity))
	quoteCounts := app.Service.GetQuoteCounts(collectPostIds(allActivity))
	profileKey := strings.ToLower(username)
	usernames := collectUsernames(allActivity)
	usernames = append(usernames, username)
	for _, quotedPost := range quotedPosts {
		usernames = append(usernames, quotedPost.Username)
	}
	humanStatuses := app.Service.GetHumanStatusBatch(common.RemoveStringDuplicates(usernames))
	timelinePosts := app.buildTimelinePosts(posts, sanitizer, humanStatuses, quotedPosts, quoteCounts)
	feedPosts := app.buildTimelinePosts(feed, sanitizer, humanStatuses, quotedPosts, quoteCounts)

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
		quotedPosts, quoteCounts, humanStatuses := app.postResponseContext(posts)
		timelinePosts = app.buildTimelinePosts(posts, sanitizer, humanStatuses, quotedPosts, quoteCounts)
	}
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
		SiteName:         app.Environment.SiteName,
		BaseUrl:          app.Environment.BaseUrl,
		MaxPostLength:    app.Environment.MaxPostLength,
		AllowedReactions: []string{"like", "boost", "laugh", "hmm"},
		ActiveNav:        "about",
		RecentlyJoined:   recentlyJoined,
		Trending:         trending,
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
