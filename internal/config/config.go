// Package config reads process configuration from the environment once, at startup.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type Config struct {
	DatabaseURL string
	HTTPAddr    string
	// BaseURL is the public origin, without a trailing slash, e.g. https://story.example.com.
	BaseURL string

	SMTP SMTP
	OIDC *OIDC // nil when SSO is off
}

type SMTP struct {
	Addr     string // empty: emails are logged, not sent
	Username string
	Password string
	From     string
}

type OIDC struct {
	Issuer       string
	DiscoveryURL string
	ClientID     string
	ClientSecret string
	Name         string
}

// SecureCookies is true when the public origin is https.
func (c Config) SecureCookies() bool { return strings.HasPrefix(c.BaseURL, "https://") }

// FromEnv takes os.Getenv (or a stub in tests) and returns a validated Config.
func FromEnv(getenv func(string) string) (Config, error) {
	or := func(k, def string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return def
	}
	c := Config{
		DatabaseURL: getenv("OMS_DATABASE_URL"),
		HTTPAddr:    or("OMS_HTTP_ADDR", ":8080"),
		SMTP: SMTP{
			Addr:     getenv("OMS_SMTP_ADDR"),
			Username: getenv("OMS_SMTP_USERNAME"),
			Password: getenv("OMS_SMTP_PASSWORD"),
			From:     or("OMS_MAIL_FROM", "oh-my-story <no-reply@localhost>"),
		},
	}

	if c.DatabaseURL == "" {
		return Config{}, errors.New("OMS_DATABASE_URL is required")
	}
	if u, err := url.Parse(c.DatabaseURL); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return Config{}, errors.New("OMS_DATABASE_URL must be a postgres:// URL")
	}
	base, err := parseOrigin(or("OMS_BASE_URL", "http://localhost:8080"))
	if err != nil {
		return Config{}, errors.New("OMS_BASE_URL must be an http(s) origin such as https://story.example.com")
	}
	c.BaseURL = base

	if issuer := getenv("OMS_OIDC_ISSUER"); issuer != "" {
		o := &OIDC{
			Issuer:       issuer,
			DiscoveryURL: or("OMS_OIDC_DISCOVERY_URL", issuer),
			ClientID:     getenv("OMS_OIDC_CLIENT_ID"),
			ClientSecret: getenv("OMS_OIDC_CLIENT_SECRET"),
			Name:         or("OMS_OIDC_NAME", "SSO"),
		}
		if o.ClientID == "" || o.ClientSecret == "" {
			return Config{}, fmt.Errorf("OMS_OIDC_CLIENT_ID and OMS_OIDC_CLIENT_SECRET are required when OMS_OIDC_ISSUER is set")
		}
		c.OIDC = o
	}
	return c, nil
}

// parseOrigin returns the origin in the form browsers send in the Origin
// header: lowercase scheme and host, no default port, no path.
func parseOrigin(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("not an origin")
	}
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host, nil
}
