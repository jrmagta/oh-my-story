package httpapi_test

import (
	"bufio"
	"bytes"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jrmagta/oh-my-story/api"
	"github.com/jrmagta/oh-my-story/internal/httpapi"
	"github.com/jrmagta/oh-my-story/internal/store"
)

// openAPIOperations reads "METHOD /path" for every operation in the
// description. The file keeps one layout: paths two spaces in under "paths:",
// their methods four spaces in.
func openAPIOperations(t *testing.T) []string {
	var out []string
	var path string
	inPaths := false
	sc := bufio.NewScanner(bytes.NewReader(api.OpenAPI))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && line != "" && !strings.HasPrefix(line, " "):
			inPaths = false
		case inPaths && strings.HasPrefix(line, "  /") && strings.HasSuffix(line, ":"):
			path = strings.TrimSuffix(strings.TrimSpace(line), ":")
		case inPaths && len(line) > 4 && strings.HasPrefix(line, "    ") && line[4] != ' ' && strings.HasSuffix(line, ":"):
			method := strings.TrimSuffix(strings.TrimSpace(line), ":")
			if slices.Contains([]string{"get", "post", "put", "patch", "delete"}, method) {
				out = append(out, strings.ToUpper(method)+" "+path)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestOpenAPICoversRoutes holds the description and the server to the same
// set of /api endpoints, both ways.
func TestOpenAPICoversRoutes(t *testing.T) {
	var routes []string
	for _, r := range httpapi.Routes() {
		if _, p, _ := strings.Cut(r, " "); strings.HasPrefix(p, "/api/") {
			routes = append(routes, r)
		}
	}
	described := openAPIOperations(t)
	if len(described) < 50 {
		t.Fatalf("read only %d operations from openapi.yaml; has its layout changed?", len(described))
	}
	for _, r := range routes {
		if !slices.Contains(described, r) {
			t.Errorf("route %q is not in api/openapi.yaml", r)
		}
	}
	for _, d := range described {
		if !slices.Contains(routes, d) {
			t.Errorf("api/openapi.yaml describes %q, which the server does not serve", d)
		}
	}
}

func TestOpenAPIServed(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	res, err := http.Get(h.base + "/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var b bytes.Buffer
	_, _ = b.ReadFrom(res.Body)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/yaml") || !bytes.Equal(b.Bytes(), api.OpenAPI) {
		t.Errorf("GET /api/openapi.yaml: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
}

// openAPISchemaFields reads each component schema's top-level property names.
// Layout: schema names four spaces in under "  schemas:", property names eight.
func openAPISchemaFields(t *testing.T) map[string][]string {
	out := map[string][]string{}
	var name string
	inSchemas, inProps := false, false
	sc := bufio.NewScanner(bytes.NewReader(api.OpenAPI))
	for sc.Scan() {
		line := sc.Text()
		indent := len(line) - len(strings.TrimLeft(line, " "))
		key, isKey := strings.CutSuffix(strings.TrimSpace(line), ":")
		switch {
		case line == "  schemas:":
			inSchemas = true
		case inSchemas && indent <= 2 && line != "":
			inSchemas = false
		case inSchemas && indent == 4 && isKey:
			name, inProps = key, false
			out[name] = nil
		case inSchemas && indent == 6:
			inProps = line == "      properties:"
		case inSchemas && inProps && indent == 8 && isKey:
			out[name] = append(out[name], key)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// jsonFields lists the JSON names a struct sends, including embedded structs' fields.
func jsonFields(t reflect.Type) []string {
	var out []string
	for f := range t.Fields() {
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		switch {
		case tag == "-" || !f.IsExported():
		case f.Anonymous && tag == "":
			out = append(out, jsonFields(f.Type)...)
		case tag != "":
			out = append(out, tag)
		}
	}
	return out
}

// TestOpenAPISchemasMatchStructs holds each schema's fields to the Go struct
// the handlers send, so a field added or renamed in one shows in the other.
func TestOpenAPISchemasMatchStructs(t *testing.T) {
	schemas := openAPISchemaFields(t)
	structs := map[string]any{
		"User": store.User{}, "Workspace": store.Workspace{}, "Membership": store.Membership{}, "Member": store.Member{},
		"Invite": store.Invite{}, "Workflow": store.Workflow{}, "Status": store.Status{}, "Label": store.Label{},
		"Persona": store.Persona{}, "Map": store.Map{}, "Node": store.Node{}, "Release": store.Release{}, "Step": store.Step{},
		"Placement": store.Placement{}, "Dependency": store.Dependency{}, "StepRef": store.StepRef{}, "Elsewhere": store.Elsewhere{},
		"MapView": store.MapView{}, "SharedMap": store.SharedMap{}, "Comment": store.Comment{}, "ShareLink": store.ShareLink{},
		"ApiToken": store.APIToken{}, "SearchHit": store.SearchHit{}, "SavedView": store.SavedView{},
		"HistoryEntry": store.HistoryEntry{}, "History": store.History{},
		"Webhook": store.Webhook{}, "Delivery": store.Delivery{},
		"DirectoryUser": httpapi.DirectoryUser{},
	}
	for name, v := range structs {
		got, ok := schemas[name]
		if !ok {
			t.Errorf("api/openapi.yaml has no schema %s", name)
			continue
		}
		want := jsonFields(reflect.TypeOf(v))
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("schema %s has fields %v; the Go struct sends %v", name, got, want)
		}
	}
}
