package service

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/boyter/pincer/common"
	"github.com/rs/zerolog/log"
	"os"
	"path/filepath"
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
	asyncWork              sync.WaitGroup
	derivedState           atomic.Value
	rebuildInFlight        int32
	rebuildPending         int32
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
	ser.buildDerivedState()

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
	return s.UserActivity[user].Activity
}

func (s *Service) IncrementSearchCount() {
	atomic.AddInt64(&s.SearchCount, 1)
}

func (s *Service) IncrementApiRequests() {
	atomic.AddInt64(&s.ApiRequests, 1)
}

func (s *Service) runAsync(fn func()) {
	s.asyncWork.Add(1)
	go func() {
		defer s.asyncWork.Done()
		fn()
	}()
}

func (s *Service) WaitForAsyncWork() {
	s.asyncWork.Wait()
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

	return s.LocalActivity
}

func (s *Service) AddUserActivity(user string, activity ActivityObject) {
	s.ServiceMutex.Lock()

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

	s.ServiceMutex.Unlock()
	s.refreshDerivedStateAsync()
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

	s.refreshDerivedStateAsync()
	s.runAsync(s.SaveBots)
}

func (s *Service) CreatePost(author, content, inReplyTo string) (ActivityObject, error) {
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

	s.ServiceMutex.Unlock()
	s.refreshDerivedStateAsync()
	return purged, bannedList
}

func (s *Service) GetPost(postId string) (ActivityObject, bool) {
	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	for _, a := range s.LocalActivity {
		if a.PostId == postId {
			return a, true
		}
	}
	return ActivityObject{}, false
}

func (s *Service) DeletePost(postId string) bool {
	s.ServiceMutex.Lock()

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

	s.ServiceMutex.Unlock()
	if found {
		s.refreshDerivedStateAsync()
	}
	return found
}

func (s *Service) GetThread(postId string) []ActivityObject {
	s.ServiceMutex.RLock()
	defer s.ServiceMutex.RUnlock()

	var result []ActivityObject
	for _, a := range s.LocalActivity {
		if a.PostId == postId || a.InReplyTo == postId {
			result = append(result, a)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UnixTimestamp < result[j].UnixTimestamp
	})

	return result
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
		result = append(result, s.LocalActivity[i])
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
			result = append(result, a)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UnixTimestamp > result[j].UnixTimestamp
	})

	return result
}

func (s *Service) LikePost(postId, username string) error {
	s.ServiceMutex.Lock()

	found := false
	for i := range s.LocalActivity {
		if s.LocalActivity[i].PostId == postId {
			for _, u := range s.LocalActivity[i].LikedBy {
				if u == username {
					s.ServiceMutex.Unlock()
					return errors.New("already liked this post")
				}
			}
			s.LocalActivity[i].LikedBy = append(s.LocalActivity[i].LikedBy, username)
			found = true
			break
		}
	}

	s.ServiceMutex.Unlock()

	if !found {
		return errors.New("post not found")
	}

	s.refreshDerivedStateAsync()
	s.runAsync(s.SaveActivity)
	return nil
}

func (s *Service) UnlikePost(postId, username string) error {
	s.ServiceMutex.Lock()

	found := false
	for i := range s.LocalActivity {
		if s.LocalActivity[i].PostId == postId {
			var updated []string
			for _, u := range s.LocalActivity[i].LikedBy {
				if u == username {
					found = true
				} else {
					updated = append(updated, u)
				}
			}
			if !found {
				s.ServiceMutex.Unlock()
				return errors.New("not liked by this user")
			}
			s.LocalActivity[i].LikedBy = updated
			break
		}
	}

	s.ServiceMutex.Unlock()

	if !found {
		return errors.New("post not found")
	}

	s.refreshDerivedStateAsync()
	s.runAsync(s.SaveActivity)
	return nil
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

	s.refreshDerivedStateAsync()
	s.runAsync(s.SaveBots)

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

	s.runAsync(s.SaveBots)
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

	s.runAsync(s.SaveBots)
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
		isFollowed := followSet[strings.ToLower(a.Username)]

		if isMention || isReply || isFollowed {
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

	s.runAsync(s.SaveBots)
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

	s.refreshDerivedStateAsync()
	s.runAsync(s.SaveBots)
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

	s.refreshDerivedStateAsync()
	s.runAsync(s.SaveBots)
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

	snapshot := s.ensureDerivedStateSnapshot()
	totalBots := 0
	counts := make(map[string]int, len(usernames))
	if snapshot != nil {
		totalBots = snapshot.TotalBots
		for key := range lookup {
			counts[key] = snapshot.HumanVouchCounts[key]
		}
	}

	for _, u := range usernames {
		key := strings.ToLower(u)
		result[key] = computeHumanTier(counts[key], totalBots)
	}

	return result
}

func (s *Service) SaveBots() {
	if strings.TrimSpace(s.Environment.BotsFilePath) == "" {
		return
	}

	s.ServiceMutex.RLock()
	b, err := json.MarshalIndent(s.RegisteredBots, "", "  ")
	s.ServiceMutex.RUnlock()

	if err != nil {
		log.Error().Str(common.UniqueCode, "c5d6e7f8").Err(err).Msg("error marshalling bots")
		return
	}

	s.botsSaveMutex.Lock()
	defer s.botsSaveMutex.Unlock()

	tmpDir := filepath.Dir(s.Environment.BotsFilePath)
	if tmpDir == "" {
		tmpDir = "."
	}

	tmp, err := os.CreateTemp(tmpDir, tempPrefixForPath(s.Environment.BotsFilePath)+".tmp.*")
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

func (s *Service) SaveActivity() {
	if strings.TrimSpace(s.Environment.ActivityFilePath) == "" {
		return
	}

	s.ServiceMutex.RLock()
	snapshot := activitySnapshot{
		LocalActivity: make([]ActivityObject, len(s.LocalActivity)),
		UserActivity:  s.UserActivity,
		TotalActivity: s.TotalActivity,
	}
	copy(snapshot.LocalActivity, s.LocalActivity)
	s.ServiceMutex.RUnlock()

	b, err := json.Marshal(snapshot)
	if err != nil {
		log.Error().Str(common.UniqueCode, "a1b2c3d5").Err(err).Msg("error marshalling activity")
		return
	}

	s.activitySaveMutex.Lock()
	defer s.activitySaveMutex.Unlock()

	tmpDir := filepath.Dir(s.Environment.ActivityFilePath)
	if tmpDir == "" {
		tmpDir = "."
	}

	tmp, err := os.CreateTemp(tmpDir, tempPrefixForPath(s.Environment.ActivityFilePath)+".tmp.*")
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
