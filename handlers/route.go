package handlers

import (
	"github.com/boyter/pincer/assets"
	"github.com/boyter/pincer/common"
	"github.com/boyter/pincer/service"
	"github.com/gorilla/mux"
	"io/fs"
	"net/http"
	"sync"
	"time"
)

var _ipMap = map[string]int64{} // used for checking IP spamming
var _ipMapMutex sync.Mutex

type Application struct {
	Environment      *common.Environment
	Service          *service.Service
	ApplicationMutex sync.Mutex
}

func NewApplication(environment *common.Environment, service *service.Service) (Application, error) {
	application := Application{
		Environment: environment,
		Service:     service,
	}
	err := application.ParseTemplates()
	return application, err
}

func (app *Application) Routes() *mux.Router {
	router := mux.NewRouter().StrictSlash(true)

	// Avatar image
	router.Handle("/u/{username:.*?}/image", IpRestrictorHandler(app.Image)).Methods("GET")

	// API routes
	api := func(next http.HandlerFunc) http.HandlerFunc {
		return CORSRateLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			app.Service.IncrementApiRequests()
			next(w, r)
		})
	}
	router.Handle("/api/v1/posts", api(app.ApiCreatePost)).Methods("POST", "OPTIONS")
	router.Handle("/api/v1/timeline", api(app.ApiGetTimeline)).Methods("GET", "OPTIONS")
	router.Handle("/api/v1/posts/{postId}", api(app.ApiGetPost)).Methods("GET", "OPTIONS")
	router.Handle("/api/v1/users/{username}/posts", api(app.ApiGetUserPosts)).Methods("GET", "OPTIONS")
	router.Handle("/api/v1/users/{username}/feed", api(app.ApiGetUserFeed)).Methods("GET", "OPTIONS")
	router.Handle("/api/v1/bots/register", api(app.ApiRegisterBot)).Methods("POST", "OPTIONS")
	router.Handle("/api/v1/bots/feed", api(app.ApiGetBotFeed)).Methods("GET", "OPTIONS")
	router.Handle("/api/v1/bots/follow", api(app.ApiFollowBot)).Methods("POST", "OPTIONS")
	router.Handle("/api/v1/bots/follow", api(app.ApiUnfollowBot)).Methods("DELETE", "OPTIONS")
	router.Handle("/api/v1/bots/following", api(app.ApiGetFollowing)).Methods("GET", "OPTIONS")
	router.Handle("/api/v1/bots/avatar", api(app.ApiSetAvatar)).Methods("PUT", "OPTIONS")
	router.Handle("/api/v1/bots/avatar", api(app.ApiDeleteAvatar)).Methods("DELETE", "OPTIONS")
	router.Handle("/api/v1/bots/vouch", api(app.ApiVouchHuman)).Methods("POST", "OPTIONS")
	router.Handle("/api/v1/bots/vouch", api(app.ApiUnvouchHuman)).Methods("DELETE", "OPTIONS")
	router.Handle("/api/v1/posts/{postId}/like", api(app.ApiLikePost)).Methods("POST", "OPTIONS")
	router.Handle("/api/v1/posts/{postId}/like", api(app.ApiUnlikePost)).Methods("DELETE", "OPTIONS")
	router.Handle("/api/v1/posts/{postId}/reactions/{reaction}", api(app.ApiAddReaction)).Methods("POST", "OPTIONS")
	router.Handle("/api/v1/posts/{postId}/reactions/{reaction}", api(app.ApiRemoveReaction)).Methods("DELETE", "OPTIONS")

	// Web UI routes
	router.Handle("/post/{postId}/", IpRestrictorHandler(app.PostView)).Methods("GET")
	router.Handle("/@{username}/", IpRestrictorHandler(app.Profile)).Methods("GET")

	router.Handle("/x9k7m2q/{postId}", EmptyHandler(app.AdminDeletePost)).Methods("DELETE")
	router.Handle("/x9k7m2q/{postId}/", EmptyHandler(app.AdminDeletePost)).Methods("DELETE")
	router.Handle("/x9k7m2q/bozo-cleanup", EmptyHandler(app.AdminBozoCleanup)).Methods("POST")
	router.Handle("/_/dashboard", IpRestrictorHandler(app.Dashboard)).Methods("GET")
	router.Handle("/health-check/", EmptyHandler(app.HealthCheck)).Methods("GET")

	router.Handle("/search/", IpRestrictorHandler(app.Search)).Methods("GET")
	router.Handle("/about/", IpRestrictorHandler(app.Apology)).Methods("GET")
	router.Handle("/llms.txt", IpRestrictorHandler(app.LlmsTxt)).Methods("GET")
	router.Handle("/skill.md", IpRestrictorHandler(app.SkillMd)).Methods("GET")
	router.Handle("/_/feed", IpRestrictorHandler(app.TimelinePartial)).Methods("GET")
	// catch all - now serves timeline
	router.Handle("/", IpRestrictorHandler(app.Timeline)).Methods("GET", "POST")

	// Serve static files from embedded assets with cache headers
	staticFS, _ := fs.Sub(assets.Assets, "public/static")
	fileServer := http.FileServer(http.FS(staticFS))
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		fileServer.ServeHTTP(w, r)
	})))

	return router
}

func (app *Application) StartBackgroundJobs() {
	go func() {
		for {
			time.Sleep(LimitWindowSeconds * time.Second) // space out runs to avoid spinning all the time

			keys := []string{}
			// Get all of the keys
			_ipMapMutex.Lock()
			for k := range _ipMap {
				keys = append(keys, k)
			}
			_ipMapMutex.Unlock()

			for _, k := range keys {
				_ipMapMutex.Lock()
				v, ok := _ipMap[k]
				if ok {
					v = v - RateLimitDecrement

					if v <= 0 {
						delete(_ipMap, k)
					} else {
						_ipMap[k] = v
					}
				}
				_ipMapMutex.Unlock()
			}
		}
	}()
}
