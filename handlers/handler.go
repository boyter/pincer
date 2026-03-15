package handlers

import (
	"github.com/boyter/pincer/common"
	"github.com/rs/zerolog/log"
	"net/http"
)

func (app *Application) Apology(w http.ResponseWriter, r *http.Request) {
	log.Info().Str(common.UniqueCode, "fee2804e").Str("ip", GetIP(r)).Msg("Apology")
	recentlyJoined, trending := app.sidebarProfiles()

	err := apologyTemplate.ExecuteTemplate(w, "apology.tmpl", DocsData{
		SiteName:       app.Environment.SiteName,
		BaseUrl:        app.Environment.BaseUrl,
		MaxPostLength:  app.Environment.MaxPostLength,
		ActiveNav:      "about",
		RecentlyJoined: recentlyJoined,
		Trending:       trending,
	})

	if err != nil {
		log.Error().Str(common.UniqueCode, "4b75b992").Str("ip", GetIP(r)).Err(err).Msg("error executing template")
		http.Error(w, "Internal Server Error", 500)
	}
}
