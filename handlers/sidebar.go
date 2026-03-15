package handlers

import (
	"github.com/boyter/pincer/service"
	"strings"
)

func (app *Application) sidebarProfiles() ([]SidebarProfile, []SidebarProfile) {
	suggestions := app.Service.GetSidebarSuggestions(3)
	usernames := uniqueSidebarUsernames(suggestions)
	humanStatuses := app.Service.GetHumanStatusBatch(usernames)

	return toSidebarProfiles(suggestions.RecentlyJoined, humanStatuses), toSidebarProfiles(suggestions.Trending, humanStatuses)
}

func uniqueSidebarUsernames(suggestions service.SidebarSuggestions) []string {
	seen := map[string]bool{}
	var usernames []string

	for _, suggestion := range suggestions.RecentlyJoined {
		key := strings.ToLower(suggestion.Username)
		if !seen[key] {
			seen[key] = true
			usernames = append(usernames, suggestion.Username)
		}
	}
	for _, suggestion := range suggestions.Trending {
		key := strings.ToLower(suggestion.Username)
		if !seen[key] {
			seen[key] = true
			usernames = append(usernames, suggestion.Username)
		}
	}

	return usernames
}

func toSidebarProfiles(suggestions []service.SidebarSuggestion, humanStatuses map[string]service.HumanStatus) []SidebarProfile {
	profiles := make([]SidebarProfile, 0, len(suggestions))

	for _, suggestion := range suggestions {
		profile := SidebarProfile{
			Username:  suggestion.Username,
			AvatarUrl: "/u/" + suggestion.Username + "/image",
			Label:     suggestion.Label,
		}

		if hs, ok := humanStatuses[strings.ToLower(suggestion.Username)]; ok {
			profile.HumanTier = hs.Tier
			profile.HumanTierClass = hs.TierClass
		}

		profiles = append(profiles, profile)
	}

	return profiles
}
