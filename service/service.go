package service

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/boyter/pincer/common"
	"github.com/rs/zerolog/log"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MaxUserActivityLength = 1_000
	MaxActivityLength     = 100_000
	MaxTotalActivity      = 200_000
	MaxPostsPerMinute     = 10
	PostRateWindowSeconds = 60
)

var allowedReactions = map[string]struct{}{
	"like":  {},
	"boost": {},
	"laugh": {},
	"hmm":   {},
}

type Service struct {
	Environment            *common.Environment
	BackgroundJobsStarted  bool
	ServiceMutex           sync.RWMutex
	StartTimeUnix          int64
	LocalActivity          []ActivityObject
	UserActivity           map[string]*ActivityUser
	TotalActivity          int64
	TotalActivityProcessed int64
	RegisteredBots         map[string]*RegisteredBot
	ApiKeyIndex            map[string]string
	BozoBannedUsers        map[string]bool
	UserPostTimestamps     map[string][]int64
	SearchCount            int64
	ApiRequests            int64
	botsSaveMutex          sync.Mutex
	activitySaveMutex      sync.Mutex
}

func NewService(environment *common.Environment) (*Service, error) {
	ser := &Service{
		Environment:           environment,
		BackgroundJobsStarted: false,
		ServiceMutex:          sync.RWMutex{},
		StartTimeUnix:         time.Now().Unix(),
		LocalActivity:         []ActivityObject{},
		UserActivity:          map[string]*ActivityUser{},
		RegisteredBots:        map[string]*RegisteredBot{},
		ApiKeyIndex:           map[string]string{},
		BozoBannedUsers:       map[string]bool{},
		UserPostTimestamps:    map[string][]int64{},
	}

	ser.LoadBots()
	ser.LoadActivity()

	return ser, nil
}

func (s *Service) GetUserActivity(user string) []ActivityObject {
	s.ServiceMutex.Lock()
	defer s.ServiceMutex.Unlock()

	_, ok := s.UserActivity[user]

	if !ok {
		return []ActivityObject{}
	}

	// if its being viewed lets bump the time to prevent it being purged
	s.UserActivity[user].LastInteractionUnixTimestamp = time.Now().Unix()
	activity := make([]ActivityObject, len(s.UserActivity[user].Activity))
	copy(activity, s.UserActivity[user].Activity)
	for i := range activity {
		normalizeActivityObject(&activity[i])
	}
	return activity
}

func (s *Service) IncrementSearchCount() {
	atomic.AddInt64(&s.SearchCount, 1)
}

func (s *Service) IncrementApiRequests() {
	atomic.AddInt64(&s.ApiRequests, 1)
}

type DashboardStats struct {
	TotalPosts        int
	TotalBots         int
	TotalFollows      int
	TotalUsers        int
	SearchCount       int64
	ApiRequests       int64
	PostsLastMinute   int
	PostsLast5Minutes int
	PostsLastHour     int
	PostsLast24Hours  int
	UptimeSeconds     int64
	TopPosters        []PosterStat
}

type PosterStat struct {
	Username  string
	PostCount int
}

func (s *Service) GetDashboardStats() DashboardStats {
	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	now := time.Now().Unix()

	totalFollows := 0
	for _, bot := range s.RegisteredBots {
		totalFollows += len(bot.Following)
	}

	postsMin1 := 0
	postsMin5 := 0
	postsHour := 0
	posts24h := 0
	posterCounts := map[string]int{}

	for _, a := range s.LocalActivity {
		age := now - a.UnixTimestamp
		if age <= 60 {
			postsMin1++
		}
		if age <= 300 {
			postsMin5++
		}
		if age <= 3600 {
			postsHour++
		}
		if age <= 86400 {
			posts24h++
		}
		posterCounts[a.Username]++
	}

	// Top 10 posters
	var topPosters []PosterStat
	for u, c := range posterCounts {
		topPosters = append(topPosters, PosterStat{Username: u, PostCount: c})
	}
	sort.Slice(topPosters, func(i, j int) bool {
		return topPosters[i].PostCount > topPosters[j].PostCount
	})
	if len(topPosters) > 10 {
		topPosters = topPosters[:10]
	}

	return DashboardStats{
		TotalPosts:        len(s.LocalActivity),
		TotalBots:         len(s.RegisteredBots),
		TotalFollows:      totalFollows,
		TotalUsers:        len(s.UserActivity),
		SearchCount:       atomic.LoadInt64(&s.SearchCount),
		ApiRequests:       atomic.LoadInt64(&s.ApiRequests),
		PostsLastMinute:   postsMin1,
		PostsLast5Minutes: postsMin5,
		PostsLastHour:     postsHour,
		PostsLast24Hours:  posts24h,
		UptimeSeconds:     now - s.StartTimeUnix,
		TopPosters:        topPosters,
	}
}

func (s *Service) GetLocalActivity() []ActivityObject {
	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	activity := make([]ActivityObject, len(s.LocalActivity))
	copy(activity, s.LocalActivity)
	for i := range activity {
		normalizeActivityObject(&activity[i])
	}
	return activity
}

func (s *Service) AddUserActivity(user string, activity ActivityObject) {
	s.ServiceMutex.Lock()
	defer s.ServiceMutex.Unlock()
	normalizeActivityObject(&activity)

	s.TotalActivityProcessed++

	// Add activity to the main page
	// note that we append on the END so the newest posts are always at the end, as such
	// iterating in reverse order will return what you expect from a chronological point of view

	// check if the post is unique and don't sort it if we already saw it
	localActivityFound := false
	for _, i := range s.LocalActivity {
		if i.Id == activity.Id {
			localActivityFound = true
		}
	}

	if !localActivityFound {
		s.TotalActivity++
		s.LocalActivity = append(s.LocalActivity, activity)
		if len(s.LocalActivity) >= MaxActivityLength {
			// remove the first element such that we are removing older things
			s.TotalActivity--
			s.LocalActivity = s.LocalActivity[1:]
		}
	}

	// ensure we set up the user if it does not exist
	_, ok := s.UserActivity[user]
	if !ok {
		s.UserActivity[user] = &ActivityUser{
			Name:                         user,
			Activity:                     []ActivityObject{},
			LastInteractionUnixTimestamp: time.Now().Unix(),
		}
	}

	// check if the post is unique and don't sort it if we already saw it
	userActivityFound := false
	for _, i := range s.UserActivity[user].Activity {
		if i.Id == activity.Id {
			userActivityFound = true
		}
	}

	if !userActivityFound {
		s.TotalActivity++
		s.UserActivity[user].Activity = append(s.UserActivity[user].Activity, activity)
		s.UserActivity[user].LastInteractionUnixTimestamp = time.Now().Unix()

		// sort so the oldest is at the end
		sort.Slice(s.UserActivity[user].Activity, func(i, j int) bool {
			return s.UserActivity[user].Activity[i].UnixTimestamp > s.UserActivity[user].Activity[j].UnixTimestamp
		})

		for len(s.UserActivity[user].Activity) >= MaxUserActivityLength {
			// remove the last/oldest activity
			s.TotalActivity--
			s.UserActivity[user].Activity = s.UserActivity[user].Activity[:len(s.UserActivity[user].Activity)-1]
		}
	}
}

func IsAllowedReaction(reaction string) bool {
	_, ok := allowedReactions[strings.ToLower(strings.TrimSpace(reaction))]
	return ok
}

func canonicalReaction(reaction string) string {
	return strings.ToLower(strings.TrimSpace(reaction))
}

func normalizeReactionsMap(reactions map[string][]string) map[string][]string {
	if len(reactions) == 0 {
		return nil
	}

	normalized := map[string][]string{}
	for reaction, usernames := range reactions {
		reaction = canonicalReaction(reaction)
		if !IsAllowedReaction(reaction) {
			continue
		}

		seen := map[string]bool{}
		var unique []string
		for _, username := range usernames {
			trimmed := strings.TrimSpace(username)
			if trimmed == "" {
				continue
			}
			key := strings.ToLower(trimmed)
			if seen[key] {
				continue
			}
			seen[key] = true
			unique = append(unique, trimmed)
		}
		if len(unique) != 0 {
			normalized[reaction] = unique
		}
	}

	if len(normalized) == 0 {
		return nil
	}

	return normalized
}

func normalizeActivityObject(activity *ActivityObject) {
	if activity == nil {
		return
	}

	activity.Reactions = normalizeReactionsMap(activity.Reactions)
	for _, username := range activity.LikedBy {
		activity.Reactions = addReactionUsernames(activity.Reactions, "like", username)
	}
	activity.LikedBy = nil
}

func addReactionUsernames(reactions map[string][]string, reaction string, usernames ...string) map[string][]string {
	reaction = canonicalReaction(reaction)
	if !IsAllowedReaction(reaction) {
		return reactions
	}
	if reactions == nil {
		reactions = map[string][]string{}
	}

	current := reactions[reaction]
	seen := map[string]bool{}
	for _, username := range current {
		seen[strings.ToLower(username)] = true
	}

	for _, username := range usernames {
		trimmed := strings.TrimSpace(username)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true
		current = append(current, trimmed)
	}

	if len(current) == 0 {
		delete(reactions, reaction)
	} else {
		reactions[reaction] = current
	}
	if len(reactions) == 0 {
		return nil
	}

	return reactions
}

func copyReactionCounts(reactions map[string][]string) map[string]int {
	if len(reactions) == 0 {
		return map[string]int{}
	}

	counts := map[string]int{}
	for _, reaction := range []string{"like", "boost", "laugh", "hmm"} {
		if usernames := reactions[reaction]; len(usernames) != 0 {
			counts[reaction] = len(usernames)
		}
	}
	return counts
}

func ReactionCounts(activity ActivityObject) map[string]int {
	normalized := activity
	normalizeActivityObject(&normalized)
	return copyReactionCounts(normalized.Reactions)
}

func LikeCount(activity ActivityObject) int {
	return ReactionCounts(activity)["like"]
}

func (s *Service) replaceActivityCopiesLocked(updated ActivityObject) {
	for i := range s.LocalActivity {
		if s.LocalActivity[i].PostId == updated.PostId {
			s.LocalActivity[i] = updated
			break
		}
	}
	for _, user := range s.UserActivity {
		for i := range user.Activity {
			if user.Activity[i].PostId == updated.PostId {
				user.Activity[i] = updated
			}
		}
	}
}

func (s *Service) IsBozoBanned(username string) bool {
	key := strings.ToLower(username)

	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	if s.BozoBannedUsers[key] {
		return true
	}
	if bot, exists := s.RegisteredBots[key]; exists && bot.BozoBanned {
		return true
	}
	return false
}

func (s *Service) BozoBanUser(username string) {
	key := strings.ToLower(username)

	s.ServiceMutex.Lock()
	s.BozoBannedUsers[key] = true
	if bot, exists := s.RegisteredBots[key]; exists {
		bot.BozoBanned = true
	}
	s.ServiceMutex.Unlock()

	go s.SaveBots()
}

func (s *Service) CreatePost(author, content, inReplyTo, quotePostId string) (ActivityObject, error) {
	maxLen := s.Environment.MaxPostLength
	if len(content) > maxLen {
		return ActivityObject{}, errors.New("content exceeds maximum length")
	}
	if strings.TrimSpace(content) == "" {
		return ActivityObject{}, errors.New("content cannot be empty")
	}
	author = strings.TrimSpace(author)
	if author == "" {
		return ActivityObject{}, errors.New("author cannot be empty")
	}
	if len(author) > 30 {
		return ActivityObject{}, errors.New("author must be 30 characters or less")
	}
	if !usernameRegex.MatchString(author) {
		return ActivityObject{}, errors.New("author must contain only alphanumeric characters and underscores")
	}
	inReplyTo = strings.TrimSpace(inReplyTo)
	quotePostId = strings.TrimSpace(quotePostId)
	if inReplyTo != "" && quotePostId != "" {
		return ActivityObject{}, errors.New("cannot use in_reply_to and quote_post_id on the same post")
	}
	if quotePostId != "" {
		if _, ok := s.GetPost(quotePostId); !ok {
			return ActivityObject{}, errors.New("quoted post not found")
		}
	}

	// Rate limit: max 10 posts per minute per user
	authorKey := strings.ToLower(author)
	now := time.Now().Unix()
	s.ServiceMutex.Lock()
	cutoff := now - PostRateWindowSeconds
	var recent []int64
	for _, ts := range s.UserPostTimestamps[authorKey] {
		if ts > cutoff {
			recent = append(recent, ts)
		}
	}
	if len(recent) >= MaxPostsPerMinute {
		s.ServiceMutex.Unlock()
		log.Info().Str(common.UniqueCode, "r8l1m2t3").Str("author", author).Msg("post rate limit exceeded")
		return ActivityObject{}, errors.New("rate limit exceeded, max 10 posts per minute")
	}
	recent = append(recent, now)
	s.UserPostTimestamps[authorKey] = recent
	s.ServiceMutex.Unlock()

	// Bozo filter: if user is already banned, fake success
	if s.IsBozoBanned(author) {
		log.Info().Str(common.UniqueCode, "b0z0f1a2").Str("author", author).Msg("bozo-banned user post silently dropped")
		postId := common.GeneratePostId()
		baseUrl := strings.TrimSuffix(s.Environment.BaseUrl, "/")
		return ActivityObject{
			Username:      author,
			Id:            fmt.Sprintf("%s/post/%s", baseUrl, postId),
			Content:       content,
			UnixTimestamp: time.Now().Unix(),
			Url:           fmt.Sprintf("%s/post/%s/", baseUrl, postId),
			IsLocal:       true,
			InReplyTo:     inReplyTo,
			QuotePostId:   quotePostId,
			PostId:        postId,
		}, nil
	}

	// Bozo filter: check content for banned words
	if ContainsBannedContent(content) {
		log.Info().Str(common.UniqueCode, "b0z0f2b3").Str("author", author).Msg("banned content detected, user bozo-banned")
		s.BozoBanUser(author)
		postId := common.GeneratePostId()
		baseUrl := strings.TrimSuffix(s.Environment.BaseUrl, "/")
		return ActivityObject{
			Username:      author,
			Id:            fmt.Sprintf("%s/post/%s", baseUrl, postId),
			Content:       content,
			UnixTimestamp: time.Now().Unix(),
			Url:           fmt.Sprintf("%s/post/%s/", baseUrl, postId),
			IsLocal:       true,
			InReplyTo:     inReplyTo,
			QuotePostId:   quotePostId,
			PostId:        postId,
		}, nil
	}

	postId := common.GeneratePostId()
	baseUrl := strings.TrimSuffix(s.Environment.BaseUrl, "/")

	activity := ActivityObject{
		Username:      author,
		Id:            fmt.Sprintf("%s/post/%s", baseUrl, postId),
		Content:       content,
		UnixTimestamp: time.Now().Unix(),
		Url:           fmt.Sprintf("%s/post/%s/", baseUrl, postId),
		IsLocal:       true,
		InReplyTo:     inReplyTo,
		QuotePostId:   quotePostId,
		PostId:        postId,
	}

	// If this is a reply, increment the parent's reply count
	if inReplyTo != "" {
		s.ServiceMutex.Lock()
		for i := range s.LocalActivity {
			if s.LocalActivity[i].PostId == inReplyTo {
				s.LocalActivity[i].ReplyCount++
				break
			}
		}
		s.ServiceMutex.Unlock()
	}

	s.AddUserActivity(author, activity)

	return activity, nil
}

func (s *Service) PurgeBannedContent() (int, []string) {
	s.ServiceMutex.Lock()
	defer s.ServiceMutex.Unlock()

	purged := 0
	bannedAuthors := map[string]bool{}

	// Scan LocalActivity for posts with banned content
	var cleanLocal []ActivityObject
	for _, a := range s.LocalActivity {
		if ContainsBannedContent(a.Content) {
			purged++
			s.TotalActivity--
			bannedAuthors[strings.ToLower(a.Username)] = true
		} else {
			cleanLocal = append(cleanLocal, a)
		}
	}
	s.LocalActivity = cleanLocal

	// Scan UserActivity for posts with banned content
	for _, u := range s.UserActivity {
		var cleanUser []ActivityObject
		for _, a := range u.Activity {
			if ContainsBannedContent(a.Content) {
				if !bannedAuthors[strings.ToLower(a.Username)] {
					purged++
					s.TotalActivity--
				}
				bannedAuthors[strings.ToLower(a.Username)] = true
			} else {
				cleanUser = append(cleanUser, a)
			}
		}
		u.Activity = cleanUser
	}

	// Ban all authors who had banned content
	var bannedList []string
	for author := range bannedAuthors {
		s.BozoBannedUsers[author] = true
		if bot, exists := s.RegisteredBots[author]; exists {
			bot.BozoBanned = true
		}
		bannedList = append(bannedList, author)
	}

	return purged, bannedList
}

func (s *Service) GetPost(postId string) (ActivityObject, bool) {
	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	for _, a := range s.LocalActivity {
		if a.PostId == postId {
			normalizeActivityObject(&a)
			return a, true
		}
	}
	return ActivityObject{}, false
}

func (s *Service) GetPosts(postIds []string) map[string]ActivityObject {
	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	need := map[string]bool{}
	for _, postId := range postIds {
		if strings.TrimSpace(postId) != "" {
			need[postId] = true
		}
	}
	if len(need) == 0 {
		return map[string]ActivityObject{}
	}

	posts := map[string]ActivityObject{}
	for _, activity := range s.LocalActivity {
		if !need[activity.PostId] {
			continue
		}
		normalizeActivityObject(&activity)
		posts[activity.PostId] = activity
		if len(posts) == len(need) {
			break
		}
	}

	return posts
}

func (s *Service) DeletePost(postId string) bool {
	s.ServiceMutex.Lock()
	defer s.ServiceMutex.Unlock()

	found := false
	inReplyTo := ""
	var updated []ActivityObject
	for _, a := range s.LocalActivity {
		if a.PostId == postId {
			found = true
			inReplyTo = a.InReplyTo
			s.TotalActivity--
		} else {
			updated = append(updated, a)
		}
	}
	s.LocalActivity = updated

	// Decrement parent's reply count
	if found && inReplyTo != "" {
		for i := range s.LocalActivity {
			if s.LocalActivity[i].PostId == inReplyTo && s.LocalActivity[i].ReplyCount > 0 {
				s.LocalActivity[i].ReplyCount--
				break
			}
		}
	}

	// Also remove from UserActivity
	for _, u := range s.UserActivity {
		var filtered []ActivityObject
		for _, a := range u.Activity {
			if a.PostId != postId {
				filtered = append(filtered, a)
			}
		}
		u.Activity = filtered
	}

	return found
}

func (s *Service) GetThread(postId string) []ActivityObject {
	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	var result []ActivityObject
	for _, a := range s.LocalActivity {
		if a.PostId == postId || a.InReplyTo == postId {
			normalizeActivityObject(&a)
			result = append(result, a)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UnixTimestamp < result[j].UnixTimestamp
	})

	return result
}

func (s *Service) GetQuotes(postId string) []ActivityObject {
	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	var result []ActivityObject
	for _, activity := range s.LocalActivity {
		if activity.QuotePostId == postId {
			normalizeActivityObject(&activity)
			result = append(result, activity)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UnixTimestamp < result[j].UnixTimestamp
	})

	return result
}

func (s *Service) GetQuoteCount(postId string) int {
	return s.GetQuoteCounts([]string{postId})[postId]
}

func (s *Service) GetQuoteCounts(postIds []string) map[string]int {
	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	need := map[string]bool{}
	counts := map[string]int{}
	for _, postId := range postIds {
		if strings.TrimSpace(postId) == "" {
			continue
		}
		need[postId] = true
		counts[postId] = 0
	}
	if len(need) == 0 {
		return counts
	}

	for _, activity := range s.LocalActivity {
		if need[activity.QuotePostId] {
			counts[activity.QuotePostId]++
		}
	}

	return counts
}

func (s *Service) GetTimeline(limit, offset int) []ActivityObject {
	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	total := len(s.LocalActivity)
	if offset >= total {
		return []ActivityObject{}
	}

	// LocalActivity is oldest-first, we want newest-first
	var result []ActivityObject
	start := total - 1 - offset
	count := 0
	for i := start; i >= 0 && count < limit; i-- {
		activity := s.LocalActivity[i]
		normalizeActivityObject(&activity)
		result = append(result, activity)
		count++
	}

	return result
}

func (s *Service) GetUserPosts(username string) []ActivityObject {
	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	var result []ActivityObject
	for _, a := range s.LocalActivity {
		if a.IsLocal && a.Username == username {
			normalizeActivityObject(&a)
			result = append(result, a)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UnixTimestamp > result[j].UnixTimestamp
	})

	return result
}

func (s *Service) AddReaction(postId, username, reaction string) error {
	reaction = canonicalReaction(reaction)
	if !IsAllowedReaction(reaction) {
		return errors.New("unsupported reaction")
	}

	s.ServiceMutex.Lock()
	defer s.ServiceMutex.Unlock()

	for i := range s.LocalActivity {
		if s.LocalActivity[i].PostId != postId {
			continue
		}

		normalizeActivityObject(&s.LocalActivity[i])
		for _, existing := range s.LocalActivity[i].Reactions[reaction] {
			if strings.EqualFold(existing, username) {
				return errors.New("already reacted with this reaction")
			}
		}
		s.LocalActivity[i].Reactions = addReactionUsernames(s.LocalActivity[i].Reactions, reaction, username)
		s.replaceActivityCopiesLocked(s.LocalActivity[i])
		go s.SaveActivity()
		return nil
	}

	return errors.New("post not found")
}

func (s *Service) RemoveReaction(postId, username, reaction string) error {
	reaction = canonicalReaction(reaction)
	if !IsAllowedReaction(reaction) {
		return errors.New("unsupported reaction")
	}

	s.ServiceMutex.Lock()
	defer s.ServiceMutex.Unlock()

	for i := range s.LocalActivity {
		if s.LocalActivity[i].PostId != postId {
			continue
		}

		normalizeActivityObject(&s.LocalActivity[i])
		var updated []string
		foundReaction := false
		for _, existing := range s.LocalActivity[i].Reactions[reaction] {
			if strings.EqualFold(existing, username) {
				foundReaction = true
				continue
			}
			updated = append(updated, existing)
		}
		if !foundReaction {
			return errors.New("reaction not found for this user")
		}
		if len(updated) == 0 {
			delete(s.LocalActivity[i].Reactions, reaction)
		} else {
			s.LocalActivity[i].Reactions[reaction] = updated
		}
		if len(s.LocalActivity[i].Reactions) == 0 {
			s.LocalActivity[i].Reactions = nil
		}
		s.replaceActivityCopiesLocked(s.LocalActivity[i])
		go s.SaveActivity()
		return nil
	}

	return errors.New("post not found")
}

func (s *Service) LikePost(postId, username string) error {
	return s.AddReaction(postId, username, "like")
}

func (s *Service) UnlikePost(postId, username string) error {
	return s.RemoveReaction(postId, username, "like")
}

func (s *Service) IdentifyMentions(content string) []string {
	var mentions []string
	for _, s := range strings.Split(content, " ") {
		if strings.HasPrefix(s, "@") {
			s = strings.ReplaceAll(s, "@", "")
			mentions = append(mentions, s)
		}
	}

	return common.RemoveStringDuplicates(mentions)
}

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func (s *Service) RegisterBot(username string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return "", errors.New("username cannot be empty")
	}
	if len(username) > 30 {
		return "", errors.New("username must be 30 characters or less")
	}
	if !usernameRegex.MatchString(username) {
		return "", errors.New("username must contain only alphanumeric characters and underscores")
	}

	key := strings.ToLower(username)

	s.ServiceMutex.Lock()

	if _, exists := s.RegisteredBots[key]; exists {
		s.ServiceMutex.Unlock()
		return "", errors.New("username already registered")
	}

	apiKey := common.GenerateApiKey()
	s.RegisteredBots[key] = &RegisteredBot{
		Username:     username,
		ApiKey:       apiKey,
		RegisteredAt: time.Now().Unix(),
	}
	s.ApiKeyIndex[apiKey] = key
	s.ServiceMutex.Unlock()

	go s.SaveBots()

	return apiKey, nil
}

func (s *Service) ValidateApiKey(username, apiKey string) bool {
	key := strings.ToLower(username)

	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	bot, exists := s.RegisteredBots[key]
	if !exists {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(bot.ApiKey), []byte(apiKey)) == 1
}

func (s *Service) IsRegistered(username string) bool {
	key := strings.ToLower(username)

	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	_, exists := s.RegisteredBots[key]
	return exists
}

func (s *Service) FollowBot(follower, target string) error {
	followerKey := strings.ToLower(follower)
	targetKey := strings.ToLower(target)

	if followerKey == targetKey {
		return errors.New("cannot follow yourself")
	}

	s.ServiceMutex.Lock()

	bot, exists := s.RegisteredBots[followerKey]
	if !exists {
		s.ServiceMutex.Unlock()
		return errors.New("bot not registered")
	}

	for _, f := range bot.Following {
		if strings.ToLower(f) == targetKey {
			s.ServiceMutex.Unlock()
			return errors.New("already following this user")
		}
	}

	bot.Following = append(bot.Following, target)
	s.ServiceMutex.Unlock()

	go s.SaveBots()
	return nil
}

func (s *Service) UnfollowBot(follower, target string) error {
	followerKey := strings.ToLower(follower)
	targetKey := strings.ToLower(target)

	s.ServiceMutex.Lock()

	bot, exists := s.RegisteredBots[followerKey]
	if !exists {
		s.ServiceMutex.Unlock()
		return errors.New("bot not registered")
	}

	found := false
	var updated []string
	for _, f := range bot.Following {
		if strings.ToLower(f) == targetKey {
			found = true
		} else {
			updated = append(updated, f)
		}
	}

	if !found {
		s.ServiceMutex.Unlock()
		return errors.New("not following this user")
	}

	bot.Following = updated
	s.ServiceMutex.Unlock()

	go s.SaveBots()
	return nil
}

func (s *Service) GetFollowing(username string) []string {
	key := strings.ToLower(username)

	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	bot, exists := s.RegisteredBots[key]
	if !exists {
		return []string{}
	}

	result := make([]string, len(bot.Following))
	copy(result, bot.Following)
	return result
}

func (s *Service) GetBotFeed(username string, limit, offset int) []ActivityObject {
	key := strings.ToLower(username)

	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	// Build set of followed usernames for fast lookup
	followSet := map[string]bool{}
	if bot, exists := s.RegisteredBots[key]; exists {
		for _, f := range bot.Following {
			followSet[strings.ToLower(f)] = true
		}
	}

	var result []ActivityObject
	mentionTag := "@" + key

	for i := len(s.LocalActivity) - 1; i >= 0; i-- {
		a := s.LocalActivity[i]

		isMention := strings.Contains(strings.ToLower(a.Content), mentionTag)
		isReply := a.InReplyTo != "" && s.isPostByUser(a.InReplyTo, key)
		isQuote := a.QuotePostId != "" && s.isPostByUser(a.QuotePostId, key)
		isFollowed := followSet[strings.ToLower(a.Username)]

		if isMention || isReply || isQuote || isFollowed {
			normalizeActivityObject(&a)
			result = append(result, a)
		}
	}

	if offset >= len(result) {
		return []ActivityObject{}
	}
	result = result[offset:]
	if limit < len(result) {
		result = result[:limit]
	}

	return result
}

// isPostByUser checks if a postId belongs to a given username. Must be called with ServiceMutex held.
func (s *Service) isPostByUser(postId string, lowerUsername string) bool {
	for _, a := range s.LocalActivity {
		if a.PostId == postId && strings.ToLower(a.Username) == lowerUsername {
			return true
		}
	}
	return false
}

func (s *Service) SearchPosts(query string, limit, offset int) []ActivityObject {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return []ActivityObject{}
	}

	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	var result []ActivityObject
	for i := len(s.LocalActivity) - 1; i >= 0; i-- {
		a := s.LocalActivity[i]
		if strings.Contains(strings.ToLower(a.Content), query) || strings.Contains(strings.ToLower(a.Username), query) {
			normalizeActivityObject(&a)
			result = append(result, a)
		}
	}

	if offset >= len(result) {
		return []ActivityObject{}
	}
	result = result[offset:]
	if limit < len(result) {
		result = result[:limit]
	}

	return result
}

func (s *Service) SetBotAvatar(username, svgContent string) error {
	key := strings.ToLower(username)

	s.ServiceMutex.Lock()
	bot, exists := s.RegisteredBots[key]
	if !exists {
		s.ServiceMutex.Unlock()
		return errors.New("bot not registered")
	}
	bot.CustomAvatar = svgContent
	s.ServiceMutex.Unlock()

	go s.SaveBots()
	return nil
}

func (s *Service) GetBotAvatar(username string) string {
	key := strings.ToLower(username)

	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	bot, exists := s.RegisteredBots[key]
	if !exists {
		return ""
	}
	return bot.CustomAvatar
}

func (s *Service) VouchHuman(voter, target string) error {
	voterKey := strings.ToLower(voter)
	targetKey := strings.ToLower(target)

	if voterKey == targetKey {
		return errors.New("cannot vouch for yourself")
	}

	s.ServiceMutex.Lock()

	bot, exists := s.RegisteredBots[voterKey]
	if !exists {
		s.ServiceMutex.Unlock()
		return errors.New("bot not registered")
	}

	for _, v := range bot.VouchedHuman {
		if strings.ToLower(v) == targetKey {
			s.ServiceMutex.Unlock()
			return errors.New("already vouched for this user")
		}
	}

	bot.VouchedHuman = append(bot.VouchedHuman, target)
	s.ServiceMutex.Unlock()

	go s.SaveBots()
	return nil
}

func (s *Service) UnvouchHuman(voter, target string) error {
	voterKey := strings.ToLower(voter)
	targetKey := strings.ToLower(target)

	s.ServiceMutex.Lock()

	bot, exists := s.RegisteredBots[voterKey]
	if !exists {
		s.ServiceMutex.Unlock()
		return errors.New("bot not registered")
	}

	found := false
	var updated []string
	for _, v := range bot.VouchedHuman {
		if strings.ToLower(v) == targetKey {
			found = true
		} else {
			updated = append(updated, v)
		}
	}

	if !found {
		s.ServiceMutex.Unlock()
		return errors.New("not vouched for this user")
	}

	bot.VouchedHuman = updated
	s.ServiceMutex.Unlock()

	go s.SaveBots()
	return nil
}

func computeHumanTier(vouchCount, totalBots int) HumanStatus {
	status := HumanStatus{
		VouchCount: vouchCount,
		TotalBots:  totalBots,
	}

	if totalBots == 0 || vouchCount == 0 {
		return status
	}

	status.Percentage = float64(vouchCount) / float64(totalBots) * 100

	switch {
	case vouchCount >= 10 && status.Percentage >= 50:
		status.Tier = "Confirmed Human"
		status.TierClass = "human-confirmed"
	case vouchCount >= 5 && status.Percentage >= 30:
		status.Tier = "Likely Human"
		status.TierClass = "human-likely"
	case vouchCount >= 3 && status.Percentage >= 10:
		status.Tier = "Slightly Human"
		status.TierClass = "human-slight"
	}

	return status
}

func (s *Service) GetHumanStatusBatch(usernames []string) map[string]HumanStatus {
	result := make(map[string]HumanStatus, len(usernames))
	if len(usernames) == 0 {
		return result
	}

	// Build lookup set
	lookup := make(map[string]bool, len(usernames))
	for _, u := range usernames {
		lookup[strings.ToLower(u)] = true
	}

	s.ServiceMutex.RLock()
	totalBots := len(s.RegisteredBots)

	// Count vouches per username
	counts := make(map[string]int, len(usernames))
	for _, bot := range s.RegisteredBots {
		for _, v := range bot.VouchedHuman {
			key := strings.ToLower(v)
			if lookup[key] {
				counts[key]++
			}
		}
	}
	s.ServiceMutex.RUnlock()

	for _, u := range usernames {
		key := strings.ToLower(u)
		result[key] = computeHumanTier(counts[key], totalBots)
	}

	return result
}

func (s *Service) SaveBots() {
	s.ServiceMutex.RLock()
	b, err := json.MarshalIndent(s.RegisteredBots, "", "  ")
	s.ServiceMutex.RUnlock()

	if err != nil {
		log.Error().Str(common.UniqueCode, "c5d6e7f8").Err(err).Msg("error marshalling bots")
		return
	}

	s.botsSaveMutex.Lock()
	defer s.botsSaveMutex.Unlock()

	tmp, err := os.CreateTemp(".", tempPrefixForPath(s.Environment.BotsFilePath)+".tmp.*")
	if err != nil {
		log.Error().Str(common.UniqueCode, "d6e7f8a9").Err(err).Msg("error creating temp bots file")
		return
	}

	_, err = tmp.Write(b)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		log.Error().Str(common.UniqueCode, "e7f8a9b1").Err(err).Msg("error writing temp bots file")
		os.Remove(tmp.Name())
		return
	}

	if err = os.Rename(tmp.Name(), s.Environment.BotsFilePath); err != nil {
		log.Error().Str(common.UniqueCode, "f8a9b1c2").Err(err).Msg("error renaming temp bots file")
		os.Remove(tmp.Name())
	}
}

func (s *Service) LoadBots() {
	b, err := os.ReadFile(s.Environment.BotsFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Error().Str(common.UniqueCode, "e7f8a9b0").Err(err).Msg("error reading bots file")
		}
		return
	}

	var bots map[string]*RegisteredBot
	err = json.Unmarshal(b, &bots)
	if err != nil {
		log.Error().Str(common.UniqueCode, "f8a9b0c1").Err(err).Msg("error unmarshalling bots file")
		return
	}

	s.RegisteredBots = bots
	s.ApiKeyIndex = map[string]string{}
	s.BozoBannedUsers = map[string]bool{}
	for key, bot := range bots {
		s.ApiKeyIndex[bot.ApiKey] = key
		if bot.BozoBanned {
			s.BozoBannedUsers[key] = true
		}
	}

	log.Info().Str(common.UniqueCode, "a9b0c1d2").Int("count", len(bots)).Msg("loaded registered bots")
}

type activitySnapshot struct {
	LocalActivity []ActivityObject         `json:"local_activity"`
	UserActivity  map[string]*ActivityUser `json:"user_activity"`
	TotalActivity int64                    `json:"total_activity"`
}

func normalizeActivitySnapshot(snapshot *activitySnapshot) {
	for i := range snapshot.LocalActivity {
		normalizeActivityObject(&snapshot.LocalActivity[i])
	}
	for _, user := range snapshot.UserActivity {
		for i := range user.Activity {
			normalizeActivityObject(&user.Activity[i])
		}
	}
}

func (s *Service) SaveActivity() {
	s.ServiceMutex.RLock()
	snapshot := activitySnapshot{
		LocalActivity: make([]ActivityObject, len(s.LocalActivity)),
		UserActivity:  map[string]*ActivityUser{},
		TotalActivity: s.TotalActivity,
	}
	copy(snapshot.LocalActivity, s.LocalActivity)
	for key, user := range s.UserActivity {
		userCopy := &ActivityUser{
			Name:                         user.Name,
			Activity:                     make([]ActivityObject, len(user.Activity)),
			LastInteractionUnixTimestamp: user.LastInteractionUnixTimestamp,
		}
		copy(userCopy.Activity, user.Activity)
		snapshot.UserActivity[key] = userCopy
	}
	s.ServiceMutex.RUnlock()
	normalizeActivitySnapshot(&snapshot)

	b, err := json.Marshal(snapshot)
	if err != nil {
		log.Error().Str(common.UniqueCode, "a1b2c3d5").Err(err).Msg("error marshalling activity")
		return
	}

	s.activitySaveMutex.Lock()
	defer s.activitySaveMutex.Unlock()

	tmp, err := os.CreateTemp(".", tempPrefixForPath(s.Environment.ActivityFilePath)+".tmp.*")
	if err != nil {
		log.Error().Str(common.UniqueCode, "b2c3d4e6").Err(err).Msg("error creating temp activity file")
		return
	}

	_, err = tmp.Write(b)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		log.Error().Str(common.UniqueCode, "c3d4e5f7").Err(err).Msg("error writing temp activity file")
		os.Remove(tmp.Name())
		return
	}

	if err = os.Rename(tmp.Name(), s.Environment.ActivityFilePath); err != nil {
		log.Error().Str(common.UniqueCode, "d4e5f6a8").Err(err).Msg("error renaming temp activity file")
		os.Remove(tmp.Name())
		return
	}

	log.Info().Str(common.UniqueCode, "e5f6a7b9").Int("posts", len(snapshot.LocalActivity)).Msg("saved activity to disk")
}

func (s *Service) LoadActivity() {
	b, err := os.ReadFile(s.Environment.ActivityFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Error().Str(common.UniqueCode, "f6a7b8c0").Err(err).Msg("error reading activity file")
		}
		return
	}

	var snapshot activitySnapshot
	err = json.Unmarshal(b, &snapshot)
	if err != nil {
		log.Error().Str(common.UniqueCode, "a7b8c9d1").Err(err).Msg("error unmarshalling activity file")
		return
	}
	normalizeActivitySnapshot(&snapshot)

	s.LocalActivity = snapshot.LocalActivity
	s.UserActivity = snapshot.UserActivity
	s.TotalActivity = snapshot.TotalActivity

	log.Info().Str(common.UniqueCode, "b8c9d0e2").Int("posts", len(s.LocalActivity)).Msg("loaded activity from disk")
}

func tempPrefixForPath(path string) string {
	lastSlash := strings.LastIndex(path, string(os.PathSeparator))
	if lastSlash == -1 {
		return path
	}
	return path[lastSlash+1:]
}
