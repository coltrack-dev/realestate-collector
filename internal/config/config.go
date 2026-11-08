package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL, UserAgent, AuthMode string
	ImageStorageMode, ImageCacheDir  string
	ImageMaxBytes                    int64
	ImageCacheTTL                    time.Duration
	DetailsTTL                       time.Duration
	Delay                            time.Duration
	MaxPages, MaxListings            int
}

func Load() Config {
	c := Config{
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		UserAgent:        os.Getenv("USER_AGENT"),
		AuthMode:         "none",
		Delay:            3 * time.Second,
		MaxPages:         3,
		MaxListings:      50,
		ImageStorageMode: "EXTERNAL_URL",
		ImageCacheDir:    "./var/image-cache",
		ImageMaxBytes:    20 * 1024 * 1024,
		ImageCacheTTL:    24 * time.Hour,
		DetailsTTL:       24 * time.Hour,
	}
	if c.UserAgent == "" {
		c.UserAgent = "realestate-collector/0.1"
	}
	if v := os.Getenv("AUTH_MODE"); v == "session" {
		c.AuthMode = v
	}
	if v := os.Getenv("REQUEST_DELAY"); v != "" {
		if x, e := time.ParseDuration(v); e == nil {
			c.Delay = x
		}
	}
	if v := os.Getenv("IMAGE_STORAGE_MODE"); v == "EXTERNAL_URL" || v == "TEMP_CACHE" || v == "LOCAL_STORAGE" {
		c.ImageStorageMode = v
	}
	if v := os.Getenv("IMAGE_CACHE_DIR"); v != "" {
		c.ImageCacheDir = v
	}
	if v := os.Getenv("IMAGE_MAX_BYTES"); v != "" {
		if x, e := strconv.ParseInt(v, 10, 64); e == nil && x > 0 {
			c.ImageMaxBytes = x
		}
	}
	if v := os.Getenv("IMAGE_CACHE_TTL"); v != "" {
		if x, e := time.ParseDuration(v); e == nil && x > 0 {
			c.ImageCacheTTL = x
		}
	}
	if v := os.Getenv("DETAILS_TTL"); v != "" {
		if x, e := time.ParseDuration(v); e == nil && x > 0 {
			c.DetailsTTL = x
		}
	}
	return c
}
