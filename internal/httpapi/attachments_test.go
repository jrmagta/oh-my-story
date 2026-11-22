package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// A 1x1 PNG.
var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

type fileResp struct {
	status int
	header http.Header
	body   []byte
}

// send makes a raw request: a file upload (contentType set) or a download.
func (c *client) send(method, path, contentType string, body []byte, header map[string]string) fileResp {
	c.h.t.Helper()
	req, err := http.NewRequest(method, c.h.base+path, bytes.NewReader(body))
	if err != nil {
		c.h.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.h.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return fileResp{res.StatusCode, res.Header, b}
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return m
}

func (c *client) upload(want int, stepPath, name string, data []byte) map[string]any {
	c.h.t.Helper()
	r := c.send("POST", stepPath+"/attachments?name="+url.QueryEscape(name), "application/octet-stream", data, nil)
	if r.status != want {
		c.h.t.Fatalf("upload %s: status %d, want %d: %s", name, r.status, want, r.body)
	}
	if want != 201 {
		return nil
	}
	return decode(c.h.t, r.body)
}

func TestAttachments(t *testing.T) {
	h := newHarness(t, harnessOpts{maxFile: 1 << 20})
	owner := h.bootstrap()
	b := buildMap(owner)
	editor, _ := h.join(owner, "ed@example.com", "editor")
	commenter, _ := h.join(owner, "co@example.com", "commenter")
	viewer, _ := h.join(owner, "vi@example.com", "viewer")
	step := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "Sign up", "activityId": b.act1}).body, "step", "id")
	stepPath := b.path + "/steps/" + step

	// Editors attach; the type comes from the bytes, not the name.
	img := editor.upload(201, stepPath, "screens/login.png", pngBytes)
	if str(img, "name") != "login.png" || str(img, "contentType") != "image/png" || img["size"] != float64(len(pngBytes)) ||
		str(img, "uploaderName") == "" || str(img, "stepId") != step {
		t.Errorf("uploaded = %v", img)
	}
	page := editor.upload(201, stepPath, "notes.png", []byte("<!doctype html><script>alert(1)</script>"))
	if ct := str(page, "contentType"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("an HTML file named .png has type %q", ct)
	}
	commenter.upload(403, stepPath, "x.txt", []byte("x"))
	viewer.upload(403, stepPath, "x.txt", []byte("x"))

	// Listed oldest first, and counted on the step.
	if r := viewer.must(200, "GET", stepPath+"/attachments", nil); len(r.list) != 2 || str(r.list[0], "id") != str(img, "id") {
		t.Errorf("attachments = %s", r.raw)
	}
	if s := owner.must(200, "GET", b.path, nil).body["steps"].([]any)[0]; s.(map[string]any)["attachmentCount"] != 2.0 {
		t.Errorf("step = %v", s)
	}

	// Images are shown inline; anything else downloads. Neither may be sniffed into something else.
	file := acme + "/attachments/" + str(img, "id")
	got := viewer.send("GET", file, "", nil, nil)
	if got.status != 200 || !bytes.Equal(got.body, pngBytes) || got.header.Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(got.header.Get("Content-Disposition"), "inline") || got.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("download: %d %v", got.status, got.header)
	}
	if r := viewer.send("GET", file, "", nil, map[string]string{"If-None-Match": got.header.Get("ETag")}); r.status != 304 {
		t.Errorf("If-None-Match: status %d", r.status)
	}
	html := viewer.send("GET", acme+"/attachments/"+str(page, "id"), "", nil, nil)
	if d := html.header.Get("Content-Disposition"); d != `attachment; filename=notes.png` {
		t.Errorf("an HTML file is sent with Content-Disposition %q", d)
	}

	// Bad uploads.
	editor.upload(413, stepPath, "big.bin", make([]byte, 1<<20+1))
	editor.upload(201, stepPath, "edge.bin", make([]byte, 1<<20))
	editor.upload(400, stepPath, "", []byte("x"))
	editor.upload(400, stepPath, "..", []byte("x"))
	if r := editor.send("POST", stepPath+"/attachments?name=a.txt", "application/json", []byte(`{}`), nil); r.status != 400 {
		t.Errorf("a JSON body: status %d", r.status)
	}
	// A cross-site form can send multipart; that is refused before any handler.
	if r := editor.send("POST", stepPath+"/attachments?name=a.txt", "multipart/form-data; boundary=x", []byte("--x--"), nil); r.status != 400 {
		t.Errorf("a multipart body: status %d", r.status)
	}

	// Another workspace's members cannot reach the file, even by its id.
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Beta", "slug": "beta"})
	if r := owner.send("GET", "/api/workspaces/beta/attachments/"+str(img, "id"), "", nil, nil); r.status != 404 {
		t.Errorf("through another workspace: status %d", r.status)
	}

	// Deleting hides it; restoring brings it back. History names the file.
	editor.must(204, "DELETE", b.path+"/attachments/"+str(page, "id"), nil)
	if r := viewer.send("GET", acme+"/attachments/"+str(page, "id"), "", nil, nil); r.status != 404 {
		t.Errorf("deleted file: status %d", r.status)
	}
	if r := viewer.must(200, "GET", stepPath+"/attachments", nil); len(r.list) != 2 {
		t.Errorf("after delete: %s", r.raw)
	}
	editor.must(404, "DELETE", b.path+"/attachments/"+str(page, "id"), nil)
	viewer.must(403, "POST", b.path+"/attachments/"+str(page, "id")+"/restore", nil)
	editor.must(204, "POST", b.path+"/attachments/"+str(page, "id")+"/restore", nil)
	es, _ := entries(owner.must(200, "GET", b.path+"/history?limit=2", nil))
	if es[0].entity != "attachment" || es[0].action != "restore" || es[1].action != "delete" || es[1].before["name"] != "notes.png" {
		t.Errorf("history: %+v", es)
	}
	// The step's own history lists its files too.
	if es, _ := entries(owner.must(200, "GET", stepPath+"/history?limit=1", nil)); len(es) != 1 || es[0].entity != "attachment" {
		t.Errorf("step history: %+v", es)
	}

	// A file goes and comes back with its step.
	owner.must(204, "DELETE", stepPath, nil)
	if r := viewer.send("GET", file, "", nil, nil); r.status != 404 {
		t.Errorf("file of a deleted step: status %d", r.status)
	}
	owner.must(200, "POST", stepPath+"/restore", nil)
	if r := viewer.send("GET", file, "", nil, nil); r.status != 200 {
		t.Errorf("file of a restored step: status %d", r.status)
	}
}

func TestAttachmentsOnLinkedStepsAndTokens(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	a := buildMap(owner)
	b := buildMap(owner)
	step := str(owner.must(201, "POST", a.path+"/steps", map[string]any{"title": "Sign up", "activityId": a.act1}).body, "step", "id")
	tokens := acme + "/api-tokens"
	write := h.tokenClient(str(owner.must(201, "POST", tokens, map[string]any{"name": "CI", "scope": "write"}).body, "token"))
	read := h.tokenClient(str(owner.must(201, "POST", tokens, map[string]any{"name": "Dash", "scope": "read"}).body, "token"))

	// Write tokens upload; read tokens only download.
	f := write.upload(201, a.path+"/steps/"+step, "log.txt", []byte("started\n"))
	read.upload(403, a.path+"/steps/"+step, "log.txt", []byte("x"))
	if r := read.send("GET", acme+"/attachments/"+str(f, "id"), "", nil, nil); r.status != 200 || string(r.body) != "started\n" {
		t.Errorf("read token download: %d %q", r.status, r.body)
	}

	// Not on map B yet: B cannot list or delete it.
	owner.must(404, "GET", b.path+"/steps/"+step+"/attachments", nil)
	owner.must(404, "DELETE", b.path+"/attachments/"+str(f, "id"), nil)

	// Once the step is on B too, B shows its files, and an upload is heard on both maps.
	owner.must(201, "POST", b.path+"/placements", map[string]any{"stepId": step, "activityId": b.act1})
	if r := owner.must(200, "GET", b.path+"/steps/"+step+"/attachments", nil); len(r.list) != 1 {
		t.Errorf("on map B: %s", r.raw)
	}
	write.upload(201, b.path+"/steps/"+step, "more.txt", []byte("more\n"))
	for _, m := range []string{a.path, b.path} {
		es, _ := entries(owner.must(200, "GET", m+"/history?limit=1", nil))
		if len(es) != 1 || es[0].entity != "attachment" || es[0].after["name"] != "more.txt" {
			t.Errorf("%s history: %+v", m, es)
		}
	}
}

func TestSharedAttachments(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	a := buildMap(owner)
	b := buildMap(owner)
	here := str(owner.must(201, "POST", a.path+"/steps", map[string]any{"title": "Sign up", "activityId": a.act1}).body, "step", "id")
	there := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "Pay", "activityId": b.act1}).body, "step", "id")
	img := str(owner.upload(201, a.path+"/steps/"+here, "login.png", pngBytes), "id")
	other := str(owner.upload(201, b.path+"/steps/"+there, "invoice.png", pngBytes), "id")
	link := owner.must(201, "POST", a.path+"/share-links", map[string]any{})
	url := str(link.body, "url")
	shared := "/api/shared/" + url[strings.LastIndex(url, "/s/")+3:]
	anon := h.client()

	// The link lists and sends the files of its own map's steps, without who uploaded them.
	list := anon.must(200, "GET", shared+"/steps/"+here+"/attachments", nil)
	if len(list.list) != 1 || str(list.list[0], "name") != "login.png" || list.list[0].(map[string]any)["uploadedBy"] != nil ||
		strings.Contains(list.raw, "owner@example.com") {
		t.Errorf("shared list = %s", list.raw)
	}
	r := anon.send("GET", shared+"/attachments/"+img, "", nil, nil)
	if r.status != 200 || !bytes.Equal(r.body, pngBytes) || !strings.HasPrefix(r.header.Get("Content-Disposition"), "inline") {
		t.Errorf("shared download: %d %v", r.status, r.header)
	}
	if r := anon.send("GET", shared+"/attachments/"+img, "", nil, map[string]string{"If-None-Match": r.header.Get("ETag")}); r.status != 304 {
		t.Errorf("shared revalidate: %d", r.status)
	}

	// Another map's step and file are out of reach, as is a made-up token.
	anon.must(404, "GET", shared+"/steps/"+there+"/attachments", nil)
	if r := anon.send("GET", shared+"/attachments/"+other, "", nil, nil); r.status != 404 {
		t.Errorf("other map's file: %d", r.status)
	}
	if r := anon.send("GET", "/api/shared/not-a-token/attachments/"+img, "", nil, nil); r.status != 404 {
		t.Errorf("bad token: %d", r.status)
	}

	// Once the step is on this map too, its files are.
	owner.must(201, "POST", a.path+"/placements", map[string]any{"stepId": there, "activityId": a.act2})
	if r := anon.send("GET", shared+"/attachments/"+other, "", nil, nil); r.status != 200 {
		t.Errorf("linked step's file: %d", r.status)
	}

	// A deleted step's files go with it, and a revoked link reaches nothing.
	owner.must(204, "DELETE", a.path+"/steps/"+there, nil)
	if r := anon.send("GET", shared+"/attachments/"+other, "", nil, nil); r.status != 404 {
		t.Errorf("deleted step's file: %d", r.status)
	}
	owner.must(204, "DELETE", a.path+"/share-links/"+str(link.body, "id"), nil)
	if r := anon.send("GET", shared+"/attachments/"+img, "", nil, nil); r.status != 404 {
		t.Errorf("revoked link: %d", r.status)
	}
}
