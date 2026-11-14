package config

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFromEnv(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"OMS_DATABASE_URL": "postgres://u:p@h:5432/d"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" {
		t.Errorf("default HTTPAddr = %q, want :8080", c.HTTPAddr)
	}

	for name, m := range map[string]map[string]string{
		"missing database URL": {},
		"not a postgres URL":   {"OMS_DATABASE_URL": "mysql://h/d"},
	} {
		if _, err := FromEnv(env(m)); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}
