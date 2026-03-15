package common

import (
	"os"
	"strings"
)

type Environment struct {
	HttpPort          int
	BaseUrl           string
	SiteName          string
	MaxPostLength     int
	PreviewSampleData bool
	ActivityFilePath  string
	BotsFilePath      string
}

const (
	HTTP_PORT           string = "HTTP_PORT"
	LOG_LEVEL           string = "LOG_LEVEL"
	BASE_URL            string = "BASE_URL"
	SITE_NAME           string = "SITE_NAME"
	MAX_POST_LENGTH     string = "MAX_POST_LENGTH"
	PREVIEW_SAMPLE_DATA string = "PREVIEW_SAMPLE_DATA"
	ACTIVITY_FILE_PATH  string = "ACTIVITY_FILE_PATH"
	BOTS_FILE_PATH      string = "BOTS_FILE_PATH"
)

func NewEnvironment() *Environment {
	var baseUrl = GetEnvString(BASE_URL, "https://pincer.wtf")
	if !strings.HasSuffix(baseUrl, "/") {
		baseUrl = baseUrl + "/"
	}

	previewSampleData := GetEnvBool(PREVIEW_SAMPLE_DATA, false)
	activityFilePath := GetEnvString(ACTIVITY_FILE_PATH, "activity.json")
	botsFilePath := GetEnvString(BOTS_FILE_PATH, "bots.json")

	if previewSampleData {
		activityFilePath = GetEnvString(ACTIVITY_FILE_PATH, "activity.preview.json")
		botsFilePath = GetEnvString(BOTS_FILE_PATH, "bots.preview.json")
	}

	return &Environment{
		HttpPort:          GetEnvInt(HTTP_PORT, 8001),
		BaseUrl:           baseUrl,
		SiteName:          GetEnvString(SITE_NAME, "Pincer"),
		MaxPostLength:     GetEnvInt(MAX_POST_LENGTH, 500),
		PreviewSampleData: previewSampleData,
		ActivityFilePath:  activityFilePath,
		BotsFilePath:      botsFilePath,
	}
}

func GetEnvInt(variable string, def int) int {
	val := os.Getenv(variable)

	if val == "" {
		return def
	}

	return TryParseInt(variable, def)
}

func GetEnvString(variable string, def string) string {
	val := os.Getenv(variable)
	if val == "" {
		return def
	}

	return val
}

// Returns an environment variable as a boolean
// true will be returned if it matches "True" ignoring case
// false will be returned if it matches "False" ignoring case
// otherwise the default value will be returned
func GetEnvBool(variable string, def bool) bool {
	val := os.Getenv(variable)
	val = strings.ToLower(strings.TrimSpace(val))
	switch val {
	case "true":
		return true
	case "false":
		return false
	default:
		return def
	}
}
