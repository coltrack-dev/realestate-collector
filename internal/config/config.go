package config

import (
	"os"
	"time"
)

type Config struct {
	DatabaseURL, UserAgent, AuthMode string
	Delay                            time.Duration
	MaxPages, MaxListings            int
}

func Load() Config {
	c := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		UserAgent:   os.Getenv("USER_AGENT"),
		AuthMode:    "none",
		Delay:       3 * time.Second,
		MaxPages:    3,
		MaxListings: 50,
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
	return c
}
