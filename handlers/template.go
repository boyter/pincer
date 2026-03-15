package handlers

import (
	"github.com/boyter/pincer/assets"
	"html/template"
)

var apologyTemplate *template.Template
var timelineTemplate *template.Template
var postTemplate *template.Template
var profileTemplate *template.Template
var docsTemplate *template.Template
var searchTemplate *template.Template
var dashboardTemplate *template.Template
var feedPartialTemplate *template.Template

func parsePageTemplate(page string) (*template.Template, error) {
	return template.ParseFS(assets.Assets, page, "public/html/partials/*.tmpl")
}

func (app *Application) ParseTemplates() error {
	t, err := parsePageTemplate("public/html/apology.tmpl")
	if err != nil {
		return err
	}
	apologyTemplate = t

	t, err = parsePageTemplate("public/html/timeline.tmpl")
	if err != nil {
		return err
	}
	timelineTemplate = t

	t, err = parsePageTemplate("public/html/post.tmpl")
	if err != nil {
		return err
	}
	postTemplate = t

	t, err = parsePageTemplate("public/html/profile.tmpl")
	if err != nil {
		return err
	}
	profileTemplate = t

	t, err = parsePageTemplate("public/html/docs.tmpl")
	if err != nil {
		return err
	}
	docsTemplate = t

	t, err = parsePageTemplate("public/html/search.tmpl")
	if err != nil {
		return err
	}
	searchTemplate = t

	t, err = parsePageTemplate("public/html/dashboard.tmpl")
	if err != nil {
		return err
	}
	dashboardTemplate = t

	t, err = template.ParseFS(assets.Assets, "public/html/feed_partial.tmpl")
	if err != nil {
		return err
	}
	feedPartialTemplate = t

	return nil
}
