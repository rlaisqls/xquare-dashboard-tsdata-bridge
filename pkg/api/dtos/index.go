package dtos

import (
	"html/template"

	"github.com/xquare-dashboard/pkg/services/navtree"
)

type IndexViewData struct {
	User                                *CurrentUser
	Settings                            *FrontendSettingsDTO
	AppUrl                              string
	AppSubUrl                           string
	GoogleAnalyticsId                   string
	GoogleAnalytics4Id                  string
	GoogleAnalytics4SendManualPageViews bool
	GoogleTagManagerId                  string
	NavTree                             *navtree.NavTreeRoot
	BuildVersion                        string
	BuildCommit                         string
	ThemeType                           string
	NewGrafanaVersionExists             bool
	NewGrafanaVersion                   string
	AppName                             string
	AppNameBodyClass                    string
	FavIcon                             template.URL
	AppleTouchIcon                      template.URL
	AppTitle                            string
	ContentDeliveryURL                  string
	LoadingLogo                         template.URL
	CSPContent                          string
	CSPEnabled                          bool
	IsDevelopmentEnv                    bool
	// Nonce is a cryptographic identifier for use with Content Security Policy.
	Nonce           string
	NewsFeedEnabled bool
	Assets          *EntryPointAssets
}

type EntryPointAssets struct {
	JSFiles  []EntryPointAsset
	CSSDark  string
	CSSLight string
}

type EntryPointAsset struct {
	FilePath  string
	Integrity string
}