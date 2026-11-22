package config

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

const dbURL = "postgres://u:p@h:5432/d"

func TestFromEnvDefaults(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"OMS_DATABASE_URL": dbURL}))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" || c.BaseURL != "http://localhost:8080" || c.OIDC != nil || c.SecureCookies() || c.AttachmentMaxBytes != 10<<20 {
		t.Errorf("unexpected defaults: %+v", c)
	}
}

func TestFromEnvOIDCAndHTTPS(t *testing.T) {
	c, err := FromEnv(env(map[string]string{
		"OMS_DATABASE_URL":       dbURL,
		"OMS_BASE_URL":           "https://story.example.com/",
		"OMS_OIDC_ISSUER":        "https://idp.example.com/realms/x",
		"OMS_OIDC_CLIENT_ID":     "id",
		"OMS_OIDC_CLIENT_SECRET": "secret",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://story.example.com" || !c.SecureCookies() {
		t.Errorf("base URL %q secure=%v", c.BaseURL, c.SecureCookies())
	}
	if c.OIDC == nil || c.OIDC.DiscoveryURL != c.OIDC.Issuer || c.OIDC.Name != "SSO" {
		t.Errorf("oidc = %+v", c.OIDC)
	}
}

// The CSRF check compares BaseURL to the browser's Origin header, so it must be in the same form.
func TestBaseURLNormalised(t *testing.T) {
	for in, want := range map[string]string{
		"HTTPS://Story.Example.com:443/": "https://story.example.com",
		"http://localhost:80":            "http://localhost",
		"http://localhost:8080":          "http://localhost:8080",
		"https://[::1]:8443":             "https://[::1]:8443",
	} {
		c, err := FromEnv(env(map[string]string{"OMS_DATABASE_URL": dbURL, "OMS_BASE_URL": in}))
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if c.BaseURL != want {
			t.Errorf("%s: BaseURL %q, want %q", in, c.BaseURL, want)
		}
	}
	if c, _ := FromEnv(env(map[string]string{"OMS_DATABASE_URL": dbURL, "OMS_BASE_URL": "HTTPS://x.example.com"})); !c.SecureCookies() {
		t.Error("uppercase HTTPS base URL: cookies not Secure")
	}
}

func TestFromEnvRejects(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"missing database URL":          {},
		"not a postgres URL":            {"OMS_DATABASE_URL": "mysql://h/d"},
		"base URL with a path":          {"OMS_DATABASE_URL": dbURL, "OMS_BASE_URL": "https://x.example.com/app"},
		"base URL not http":             {"OMS_DATABASE_URL": dbURL, "OMS_BASE_URL": "ftp://x"},
		"base URL with a query":         {"OMS_DATABASE_URL": dbURL, "OMS_BASE_URL": "https://x.example.com?a=1"},
		"base URL with credentials":     {"OMS_DATABASE_URL": dbURL, "OMS_BASE_URL": "https://u:p@x.example.com"},
		"OIDC issuer without creds":     {"OMS_DATABASE_URL": dbURL, "OMS_OIDC_ISSUER": "https://idp"},
		"attachment limit zero":         {"OMS_DATABASE_URL": dbURL, "OMS_ATTACHMENT_MAX_MB": "0"},
		"attachment limit not a number": {"OMS_DATABASE_URL": dbURL, "OMS_ATTACHMENT_MAX_MB": "10MB"},
	} {
		if _, err := FromEnv(env(m)); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}

func TestAttachmentLimit(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"OMS_DATABASE_URL": dbURL, "OMS_ATTACHMENT_MAX_MB": "25"}))
	if err != nil || c.AttachmentMaxBytes != 25<<20 {
		t.Errorf("AttachmentMaxBytes = %d, %v", c.AttachmentMaxBytes, err)
	}
}
