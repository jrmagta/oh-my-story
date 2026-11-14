// Package config reads process configuration from the environment once, at startup.
package config

import (
	"errors"
	"fmt"
	"net/url"
)

type Config struct {
	DatabaseURL string
	HTTPAddr    string
}

// FromEnv takes os.Getenv (or a stub in tests) and returns a validated Config.
func FromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		DatabaseURL: getenv("OMS_DATABASE_URL"),
		HTTPAddr:    getenv("OMS_HTTP_ADDR"),
	}
	if c.HTTPAddr == "" {
		c.HTTPAddr = ":8080"
	}
	if c.DatabaseURL == "" {
		return Config{}, errors.New("OMS_DATABASE_URL is required")
	}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return Config{}, fmt.Errorf("OMS_DATABASE_URL must be a postgres:// URL")
	}
	return c, nil
}
