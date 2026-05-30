package option

import "github.com/sagernet/sing/common/json/badoption"

type ExperimentalOptions struct {
	CacheFile *CacheFileOptions `json:"cache_file,omitempty"`
	ClashAPI  *ClashAPIOptions  `json:"clash_api,omitempty"`
	V2RayAPI  *V2RayAPIOptions  `json:"v2ray_api,omitempty"`
	Debug     *DebugOptions     `json:"debug,omitempty"`
	CFST      *CFSTOptions      `json:"cfst,omitempty"`
}

type CFSTOptions struct {
	Enabled       bool               `json:"enabled,omitempty"`
	RunOnStart    bool               `json:"run_on_start,omitempty"`
	Interval      badoption.Duration `json:"interval,omitempty"`
	DownloadCount int                `json:"download_count,omitempty"`
	DisplayCount  int                `json:"display_count,omitempty"`
	CacheFile     string             `json:"cache_file,omitempty"`
	CFESS         []CFESSMapping     `json:"cfess,omitempty"`
}

type CFESSMapping struct {
	Tag             string `json:"tag"`
	GroupTag        string `json:"group_tag"`
	GeneratedCount  int    `json:"generated_count,omitempty"`
	IncludeOriginal bool   `json:"include_original,omitempty"`
	StableTags      bool   `json:"stable_tags,omitempty"`
	TagTemplate     string `json:"tag_template,omitempty"`
}

type CacheFileOptions struct {
	Enabled     bool               `json:"enabled,omitempty"`
	Path        string             `json:"path,omitempty"`
	CacheID     string             `json:"cache_id,omitempty"`
	StoreFakeIP bool               `json:"store_fakeip,omitempty"`
	StoreRDRC   bool               `json:"store_rdrc,omitempty"`
	RDRCTimeout badoption.Duration `json:"rdrc_timeout,omitempty"`
}

type ClashAPIOptions struct {
	ExternalController               string                     `json:"external_controller,omitempty"`
	ExternalUI                       string                     `json:"external_ui,omitempty"`
	ExternalUIDownloadURL            string                     `json:"external_ui_download_url,omitempty"`
	ExternalUIDownloadDetour         string                     `json:"external_ui_download_detour,omitempty"`
	Secret                           string                     `json:"secret,omitempty"`
	DefaultMode                      string                     `json:"default_mode,omitempty"`
	ModeList                         []string                   `json:"-"`
	AccessControlAllowOrigin         badoption.Listable[string] `json:"access_control_allow_origin,omitempty"`
	AccessControlAllowPrivateNetwork bool                       `json:"access_control_allow_private_network,omitempty"`

	// Deprecated: migrated to global cache file
	CacheFile string `json:"cache_file,omitempty"`
	// Deprecated: migrated to global cache file
	CacheID string `json:"cache_id,omitempty"`
	// Deprecated: migrated to global cache file
	StoreMode bool `json:"store_mode,omitempty"`
	// Deprecated: migrated to global cache file
	StoreSelected bool `json:"store_selected,omitempty"`
	// Deprecated: migrated to global cache file
	StoreFakeIP bool `json:"store_fakeip,omitempty"`
}

type V2RayAPIOptions struct {
	Listen string                    `json:"listen,omitempty"`
	Stats  *V2RayStatsServiceOptions `json:"stats,omitempty"`
}

type V2RayStatsServiceOptions struct {
	Enabled   bool     `json:"enabled,omitempty"`
	Inbounds  []string `json:"inbounds,omitempty"`
	Outbounds []string `json:"outbounds,omitempty"`
	Users     []string `json:"users,omitempty"`
}
