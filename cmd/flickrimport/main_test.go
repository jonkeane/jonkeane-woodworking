package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Hello  ", "Hello"},
		{"", "Untitled"},
		{"Test Photo", "Test Photo"},
	}
	for _, tc := range cases {
		got := safeTitle(tc.in)
		if got != tc.want {
			t.Errorf("safeTitle(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestPhotoJSONUnmarshal(t *testing.T) {
	sample := `{
		"photoset": {
			"id": "123",
			"photo": [
				{
					"id": "456",
					"title": "Sunset",
					"url_h": "https://example.com/img_h.jpg",
					"datetaken": "2022-01-01 12:00:00"
				}
			],
			"page": 1,
			"pages": 1
		},
		"stat": "ok"
	}`
	var resp PhotosetsGetPhotosResp
	if err := json.NewDecoder(bytes.NewReader([]byte(sample))).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Stat != "ok" {
		t.Errorf("stat=%s; want ok", resp.Stat)
	}
	if len(resp.Photoset.Photo) != 1 {
		t.Fatalf("photo count=%d; want 1", len(resp.Photoset.Photo))
	}
	p := resp.Photoset.Photo[0]
	if p.ID != "456" || p.Title != "Sunset" || p.URLH != "https://example.com/img_h.jpg" {
		t.Errorf("photo parsed incorrectly: %+v", p)
	}
}

func TestFindWoodworkingGalleries(t *testing.T) {
	root := t.TempDir()
	for _, slug := range []string{"LP-holder", "modern-console"} {
		dir := filepath.Join(root, slug)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "_index.md"), []byte("---\ntitle: Project\nflickr_album: \"123\"\n---\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(root, "about.md"), []byte("---\ntitle: About\n---\n"), 0644)
	galleries, err := findGalleries(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(galleries) != 2 || galleries[0].Slug != "LP-holder" || galleries[1].Slug != "modern-console" {
		t.Fatalf("unexpected galleries: %+v", galleries)
	}
}

func TestAPIKeyWithoutOAuth(t *testing.T) {
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte(`{"token":"unused"}`), 0600)
	if _, err := createHTTPClient(Config{APIKey: "public-key", OAuthTokenFile: token}); err != nil {
		t.Fatal(err)
	}
	if _, err := createHTTPClient(Config{OAuthTokenFile: token}); err == nil {
		t.Fatal("missing credentials should fail before fetching")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchPhotosetPagination(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		if q.Get("photoset_id") != "123" || q.Get("method") != "flickr.photosets.getPhotos" {
			t.Fatalf("unexpected request: %v", q)
		}
		body := `{"stat":"ok","photoset":{"id":"123","page":` + q.Get("page") + `,"pages":2,"photo":[{"id":"` + q.Get("page") + `","title":"Photo"}]}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	ps, err := fetchPhotosetAll(context.Background(), client, "test-key", "123")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(ps.Photoset.Photo) != 2 || ps.Photoset.Photo[1].ID != "2" {
		t.Fatalf("pagination lost photos: %+v", ps)
	}
}

func TestExtractBlockedTag(t *testing.T) {
	tags := extractTagsFromInfo(json.RawMessage(`{"tags":{"tag":[{"raw":"nogallery"},{"raw":"hand cut joinery"}]}}`))
	if len(tags) != 2 || tags[0] != "nogallery" || tags[1] != "hand cut joinery" {
		t.Fatalf("tags changed: %v", tags)
	}
}
