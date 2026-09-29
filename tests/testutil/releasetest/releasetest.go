package releasetest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"noraegaori/internal/download"
)

const (
	SumA = "c6527f24f4b16031d3ae4fa9f658d5f11534c8d84ce7dc8502420280919c3490"
	SumB = "a0c3101b4158d1dfb7d6a78a7bf0f3de80c96bb423c152beec8beb22786f2238"
)

type Server struct {
	server    *httptest.Server
	Files     map[string]string
	Status    map[string]int
	headers   map[string]http.Header
	redirects map[string]string
}

func Serve(t *testing.T) *Server {
	t.Helper()

	fake := &Server{
		Files:     map[string]string{},
		Status:    map[string]int{},
		headers:   map[string]http.Header{},
		redirects: map[string]string{},
	}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		if target, ok := fake.redirects[key]; ok {
			http.Redirect(w, r, fake.URL(target), http.StatusFound)
			return
		}
		for name, values := range fake.headers[key] {
			w.Header()[name] = values
		}
		if code, ok := fake.Status[key]; ok {
			w.WriteHeader(code)
			return
		}
		body, ok := fake.Files[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *Server) URL(path string) string {
	return fake.server.URL + path
}

func (fake *Server) PublishRelease(t *testing.T, path string, release any) {
	t.Helper()

	body, err := json.Marshal(release)
	if err != nil {
		t.Fatalf("failed to encode the release: %v", err)
	}
	fake.Files[path] = string(body)
}

func (fake *Server) Asset(path, body string) download.Asset {
	fake.Files[path] = body
	return download.Asset{Name: path[strings.LastIndex(path, "/")+1:], BrowserDownloadURL: fake.URL(path)}
}

func (fake *Server) RateLimit(path string) {
	fake.Status[path] = http.StatusForbidden
	fake.headers[path] = http.Header{"X-Ratelimit-Remaining": {"0"}}
}

func (fake *Server) RedirectLatest(repo, tag string) {
	fake.redirects["/"+repo+"/releases/latest"] = "/" + repo + "/releases/tag/" + tag
}

type FeedEntry struct {
	Tag     string
	Updated string
}

func (fake *Server) PublishFeed(repo string, entries ...FeedEntry) {
	var feed strings.Builder
	feed.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<feed xmlns="http://www.w3.org/2005/Atom">` + "\n")
	for _, entry := range entries {
		fmt.Fprintf(&feed, "  <entry><id>tag:github.com,2008:Repository/1/%s</id><updated>%s</updated></entry>\n", entry.Tag, entry.Updated)
	}
	feed.WriteString("</feed>\n")
	fake.Files["/"+repo+"/releases.atom"] = feed.String()
}

func AcceptAll(string) bool { return true }

func RejectAll(string) bool { return false }

func RequireErrorIs(t *testing.T, err, target error) {
	t.Helper()

	if !errors.Is(err, target) {
		t.Errorf("got %v, want %v", err, target)
	}
}

func RequireOtherError(t *testing.T, err, target error) {
	t.Helper()

	if err == nil || errors.Is(err, target) {
		t.Errorf("got %v, want an error other than %v", err, target)
	}
}
