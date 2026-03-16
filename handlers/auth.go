package handlers

import (
	"encoding/json"
	"fmt"
	"github.com/boyter/pincer/common"
	"github.com/boyter/pincer/service"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"strings"
)

type RegisterBotRequest struct {
	Username string `json:"username"`
}

type RegisterBotResponse struct {
	Username string `json:"username"`
	ApiKey   string `json:"api_key"`
	Message  string `json:"message"`
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func (app *Application) ApiRegisterBot(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "a3b4c5d6").Str("ip", GetIP(r)).Msg("ApiRegisterBot")

	var req RegisterBotRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":"invalid JSON body"}`)
		return
	}

	apiKey, err := app.Service.RegisterBot(req.Username)
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		if err.Error() == "username already registered" {
			w.WriteHeader(http.StatusConflict)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = fmt.Fprint(w, string(resp))
		return
	}

	resp := RegisterBotResponse{
		Username: req.Username,
		ApiKey:   apiKey,
		Message:  "Save this key. It cannot be recovered.",
	}
	b, _ := json.MarshalIndent(resp, "", jsonIndent)
	w.Header().Set("Content-Type", common.JsonContentType)
	w.WriteHeader(http.StatusCreated)
	_, _ = fmt.Fprint(w, string(b))
}

type FollowRequest struct {
	Username string `json:"username"`
}

func (app *Application) ApiFollowBot(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "c5d6e7f9").Str("ip", GetIP(r)).Msg("ApiFollowBot")

	token := extractBearerToken(r)
	if token == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"missing or invalid Authorization header"}`)
		return
	}

	app.Service.ServiceMutex.RLock()
	follower, exists := app.Service.ApiKeyIndex[token]
	app.Service.ServiceMutex.RUnlock()

	if !exists {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"invalid API key"}`)
		return
	}

	var req FollowRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil || strings.TrimSpace(req.Username) == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":"invalid JSON body, expected {\"username\":\"...\"}"}`)
		return
	}

	err = app.Service.FollowBot(follower, req.Username)
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		if err.Error() == "already following this user" {
			w.WriteHeader(http.StatusConflict)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = fmt.Fprint(w, string(resp))
		return
	}

	w.Header().Set("Content-Type", common.JsonContentType)
	resp, _ := json.Marshal(map[string]string{"message": "now following " + req.Username})
	_, _ = fmt.Fprint(w, string(resp))
}

func (app *Application) ApiUnfollowBot(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "d6e7f8a0").Str("ip", GetIP(r)).Msg("ApiUnfollowBot")

	token := extractBearerToken(r)
	if token == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"missing or invalid Authorization header"}`)
		return
	}

	app.Service.ServiceMutex.RLock()
	follower, exists := app.Service.ApiKeyIndex[token]
	app.Service.ServiceMutex.RUnlock()

	if !exists {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"invalid API key"}`)
		return
	}

	var req FollowRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil || strings.TrimSpace(req.Username) == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":"invalid JSON body, expected {\"username\":\"...\"}"}`)
		return
	}

	err = app.Service.UnfollowBot(follower, req.Username)
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = fmt.Fprint(w, string(resp))
		return
	}

	w.Header().Set("Content-Type", common.JsonContentType)
	resp, _ := json.Marshal(map[string]string{"message": "unfollowed " + req.Username})
	_, _ = fmt.Fprint(w, string(resp))
}

func (app *Application) ApiGetFollowing(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "e7f8a9b2").Str("ip", GetIP(r)).Msg("ApiGetFollowing")

	token := extractBearerToken(r)
	if token == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"missing or invalid Authorization header"}`)
		return
	}

	app.Service.ServiceMutex.RLock()
	username, exists := app.Service.ApiKeyIndex[token]
	app.Service.ServiceMutex.RUnlock()

	if !exists {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"invalid API key"}`)
		return
	}

	following := app.Service.GetFollowing(username)
	b, _ := json.MarshalIndent(map[string]interface{}{"following": following}, "", jsonIndent)
	w.Header().Set("Content-Type", common.JsonContentType)
	_, _ = fmt.Fprint(w, string(b))
}

func (app *Application) ApiLikePost(w http.ResponseWriter, r *http.Request) {
	app.apiAddReaction(w, r, "like")
}

func (app *Application) ApiUnlikePost(w http.ResponseWriter, r *http.Request) {
	app.apiRemoveReaction(w, r, "like")
}

func (app *Application) ApiAddReaction(w http.ResponseWriter, r *http.Request) {
	app.apiAddReaction(w, r, mux.Vars(r)["reaction"])
}

func (app *Application) ApiRemoveReaction(w http.ResponseWriter, r *http.Request) {
	app.apiRemoveReaction(w, r, mux.Vars(r)["reaction"])
}

func (app *Application) authenticatedUsername(w http.ResponseWriter, r *http.Request) (string, bool) {
	token := extractBearerToken(r)
	if token == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"missing or invalid Authorization header"}`)
		return "", false
	}

	app.Service.ServiceMutex.RLock()
	username, exists := app.Service.ApiKeyIndex[token]
	app.Service.ServiceMutex.RUnlock()

	if !exists {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"invalid API key"}`)
		return "", false
	}

	return username, true
}

func (app *Application) apiAddReaction(w http.ResponseWriter, r *http.Request, reaction string) {
	vars := mux.Vars(r)
	postId := vars["postId"]
	reaction = strings.ToLower(reaction)
	log.Info().Str(common.UniqueCode, "a1c2d3e4").Str("postId", postId).Str("reaction", reaction).Str("ip", GetIP(r)).Msg("ApiAddReaction")

	username, ok := app.authenticatedUsername(w, r)
	if !ok {
		return
	}

	err := app.Service.AddReaction(postId, username, reaction)
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		switch err.Error() {
		case "already reacted with this reaction":
			w.WriteHeader(http.StatusConflict)
		case "post not found":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = fmt.Fprint(w, string(resp))
		return
	}

	w.Header().Set("Content-Type", common.JsonContentType)
	resp, _ := json.Marshal(map[string]string{"message": "added " + reaction + " to post " + postId})
	_, _ = fmt.Fprint(w, string(resp))
}

func (app *Application) apiRemoveReaction(w http.ResponseWriter, r *http.Request, reaction string) {
	vars := mux.Vars(r)
	postId := vars["postId"]
	reaction = strings.ToLower(reaction)
	log.Info().Str(common.UniqueCode, "b2d3e4f5").Str("postId", postId).Str("reaction", reaction).Str("ip", GetIP(r)).Msg("ApiRemoveReaction")

	username, ok := app.authenticatedUsername(w, r)
	if !ok {
		return
	}

	err := app.Service.RemoveReaction(postId, username, reaction)
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		if err.Error() == "post not found" {
			w.WriteHeader(http.StatusNotFound)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = fmt.Fprint(w, string(resp))
		return
	}

	w.Header().Set("Content-Type", common.JsonContentType)
	resp, _ := json.Marshal(map[string]string{"message": "removed " + reaction + " from post " + postId})
	_, _ = fmt.Fprint(w, string(resp))
}

func (app *Application) ApiVouchHuman(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "a4b5c6d7").Str("ip", GetIP(r)).Msg("ApiVouchHuman")

	token := extractBearerToken(r)
	if token == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"missing or invalid Authorization header"}`)
		return
	}

	app.Service.ServiceMutex.RLock()
	voter, exists := app.Service.ApiKeyIndex[token]
	app.Service.ServiceMutex.RUnlock()

	if !exists {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"invalid API key"}`)
		return
	}

	var req FollowRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil || strings.TrimSpace(req.Username) == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":"invalid JSON body, expected {\"username\":\"...\"}"}`)
		return
	}

	err = app.Service.VouchHuman(voter, req.Username)
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		if err.Error() == "already vouched for this user" {
			w.WriteHeader(http.StatusConflict)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = fmt.Fprint(w, string(resp))
		return
	}

	w.Header().Set("Content-Type", common.JsonContentType)
	resp, _ := json.Marshal(map[string]string{"message": "vouched " + req.Username + " as human"})
	_, _ = fmt.Fprint(w, string(resp))
}

func (app *Application) ApiUnvouchHuman(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "b5c6d7e8").Str("ip", GetIP(r)).Msg("ApiUnvouchHuman")

	token := extractBearerToken(r)
	if token == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"missing or invalid Authorization header"}`)
		return
	}

	app.Service.ServiceMutex.RLock()
	voter, exists := app.Service.ApiKeyIndex[token]
	app.Service.ServiceMutex.RUnlock()

	if !exists {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"invalid API key"}`)
		return
	}

	var req FollowRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil || strings.TrimSpace(req.Username) == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":"invalid JSON body, expected {\"username\":\"...\"}"}`)
		return
	}

	err = app.Service.UnvouchHuman(voter, req.Username)
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = fmt.Fprint(w, string(resp))
		return
	}

	w.Header().Set("Content-Type", common.JsonContentType)
	resp, _ := json.Marshal(map[string]string{"message": "unvouched " + req.Username})
	_, _ = fmt.Fprint(w, string(resp))
}

func (app *Application) ApiGetBotFeed(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "b4c5d6e7").Str("ip", GetIP(r)).Msg("ApiGetBotFeed")

	token := extractBearerToken(r)
	if token == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"missing or invalid Authorization header"}`)
		return
	}

	// Look up which bot this key belongs to
	app.Service.ServiceMutex.RLock()
	username, exists := app.Service.ApiKeyIndex[token]
	app.Service.ServiceMutex.RUnlock()

	if !exists {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"invalid API key"}`)
		return
	}

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

func (app *Application) ApiSetAvatar(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "f1a2b3c4").Str("ip", GetIP(r)).Msg("ApiSetAvatar")

	token := extractBearerToken(r)
	if token == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"missing or invalid Authorization header"}`)
		return
	}

	app.Service.ServiceMutex.RLock()
	username, exists := app.Service.ApiKeyIndex[token]
	app.Service.ServiceMutex.RUnlock()

	if !exists {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"invalid API key"}`)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, int64(service.MaxSVGSize)+1))
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":"failed to read request body"}`)
		return
	}

	sanitized, err := service.SanitizeSVG(string(body))
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = fmt.Fprint(w, string(resp))
		return
	}

	err = app.Service.SetBotAvatar(username, sanitized)
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = fmt.Fprint(w, string(resp))
		return
	}

	w.Header().Set("Content-Type", common.JsonContentType)
	w.WriteHeader(http.StatusCreated)
	_, _ = fmt.Fprint(w, `{"message":"avatar updated"}`)
}

func (app *Application) ApiDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "a2b3c4d5").Str("ip", GetIP(r)).Msg("ApiDeleteAvatar")

	token := extractBearerToken(r)
	if token == "" {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"missing or invalid Authorization header"}`)
		return
	}

	app.Service.ServiceMutex.RLock()
	username, exists := app.Service.ApiKeyIndex[token]
	app.Service.ServiceMutex.RUnlock()

	if !exists {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"invalid API key"}`)
		return
	}

	err := app.Service.SetBotAvatar(username, "")
	if err != nil {
		w.Header().Set("Content-Type", common.JsonContentType)
		w.WriteHeader(http.StatusBadRequest)
		resp, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = fmt.Fprint(w, string(resp))
		return
	}

	w.Header().Set("Content-Type", common.JsonContentType)
	_, _ = fmt.Fprint(w, `{"message":"avatar removed"}`)
}
