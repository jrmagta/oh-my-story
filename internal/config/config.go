// Package config reads process configuration from the environment once, at startup.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL string
	HTTPAddr    string
	// BaseURL is the public origin, without a trailing slash, e.g. https://story.example.com.
	BaseURL string

	SMTP SMTP
	OIDC *OIDC // nil when SSO is off

	// BlockPrivateWebhooks also stops webhooks posting to loopback and private
	// addresses (link-local and cloud metadata addresses are always refused).
	BlockPrivateWebhooks bool

	// AttachmentMaxBytes is the largest file an upload may carry.
	AttachmentMaxBytes int64
}

// DefaultAttachmentMaxBytes is 10 MB, Jira Data Center's default.
const DefaultAttachmentMaxBytes = 10 << 20

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
	// Directory turns on searching the provider's users from Members, which
	// needs the client's service account. Only "keycloak" is supported.
	Directory string
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
		DatabaseURL:          getenv("OMS_DATABASE_URL"),
		HTTPAddr:             or("OMS_HTTP_ADDR", ":8080"),
		BlockPrivateWebhooks: getenv("OMS_WEBHOOK_BLOCK_PRIVATE") == "true",
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

	c.AttachmentMaxBytes = DefaultAttachmentMaxBytes
	if v := getenv("OMS_ATTACHMENT_MAX_MB"); v != "" {
		mb, err := strconv.Atoi(v)
		if err != nil || mb < 1 || mb > 1024 {
			return Config{}, errors.New("OMS_ATTACHMENT_MAX_MB must be a whole number of megabytes from 1 to 1024")
		}
		c.AttachmentMaxBytes = int64(mb) << 20
	}
	if issuer := getenv("OMS_OIDC_ISSUER"); issuer != "" {
		o := &OIDC{
			Issuer:       issuer,
			DiscoveryURL: or("OMS_OIDC_DISCOVERY_URL", issuer),
			ClientID:     getenv("OMS_OIDC_CLIENT_ID"),
			ClientSecret: getenv("OMS_OIDC_CLIENT_SECRET"),
			Name:         or("OMS_OIDC_NAME", "SSO"),
			Directory:    getenv("OMS_OIDC_DIRECTORY"),
		}
		if o.Directory != "" && o.Directory != "keycloak" {
			return Config{}, fmt.Errorf(`OMS_OIDC_DIRECTORY must be "keycloak" or empty`)
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
