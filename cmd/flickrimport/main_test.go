package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
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
					"width_h": 1600,
					"height_h": "1067",
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
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(encoded, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["width_h"] != float64(1600) || saved["height_h"] != float64(1067) {
		t.Fatalf("thumbnail dimensions lost on import: %s", encoded)
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

func testImage(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestGalleryImageCache(t *testing.T) {
	body := testImage(t)
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Fatal("image downloads must not request gzip from Flickr")
		}
		if r.UserAgent() != "woodworking-flickrimport" {
			t.Fatal("image downloads must identify the importer")
		}
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	ps := &PhotosetsGetPhotosResp{}
	ps.Photoset.Photo = []Photo{{ID: "1", URLZ: "https://example.com/thumb.png", URLK: "https://example.com/large.png", URLH: "https://example.com/unused.png"}}
	dir := t.TempDir()
	run := func(want int) {
		t.Helper()
		if err := downloadGalleryImages(context.Background(), client, dir, ps); err != nil {
			t.Fatal(err)
		}
		if calls != want {
			t.Fatalf("download requests = %d; want %d", calls, want)
		}
	}
	run(2)
	p := &ps.Photoset.Photo[0]
	if p.ThumbnailAsset == "" || p.FullAsset == "" || p.ThumbnailAsset == p.FullAsset {
		t.Fatalf("missing distinct image paths: %+v", p)
	}
	for _, path := range []string{p.ThumbnailAsset, p.FullAsset} {
		b, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil || !bytes.Equal(b, body) {
			t.Fatalf("missing downloaded bytes: %s: %v", path, err)
		}
	}
	p.LastUpdate = "9999999999" // Tags/EXIF edits do not change the image.
	run(2)
	if err := os.Remove(filepath.Join(dir, p.ThumbnailAsset)); err != nil {
		t.Fatal(err)
	}
	run(3)
	if err := os.WriteFile(filepath.Join(dir, p.FullAsset), []byte("broken"), 0644); err != nil {
		t.Fatal(err)
	}
	run(4)
	oldFull := p.FullAsset
	p.URLK = "https://example.com/replaced.png"
	run(5)
	if oldFull == p.FullAsset {
		t.Fatal("replacement reused old image path")
	}
	// A second album/photo using the same URLs shares the downloaded files.
	ps.Photoset.Photo = append(ps.Photoset.Photo, ps.Photoset.Photo[0])
	run(5)
}

func TestGalleryImageFallbacks(t *testing.T) {
	body := testImage(t)
	for _, field := range []string{"c", "m", "l", "h", "k", "o"} {
		t.Run(field, func(t *testing.T) {
			var p Photo
			if err := json.Unmarshal([]byte(`{"id":"1","url_`+field+`":"https://example.com/fallback.png"}`), &p); err != nil {
				t.Fatal(err)
			}
			// The viewer's existing fallbacks do not include medium; give it an original.
			if field == "m" {
				p.URLO = p.URLM
			}
			ps := &PhotosetsGetPhotosResp{}
			ps.Photoset.Photo = []Photo{p}
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body))}, nil
			})}
			if err := downloadGalleryImages(context.Background(), client, t.TempDir(), ps); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || ps.Photoset.Photo[0].ThumbnailAsset != ps.Photoset.Photo[0].FullAsset {
				t.Fatal("shared fallback should download once")
			}
		})
	}
	ps := &PhotosetsGetPhotosResp{}
	ps.Photoset.Photo = []Photo{{ID: "missing"}}
	if err := downloadGalleryImages(context.Background(), nil, t.TempDir(), ps); err == nil {
		t.Fatal("missing image URLs must fail")
	}
}

type interruptedReader struct{}

func (interruptedReader) Read([]byte) (int, error) { return 0, errors.New("connection interrupted") }

func TestDownloadPhotoFailureIsAtomic(t *testing.T) {
	for _, mode := range []string{"http", "html", "empty", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "photo.png")
			original := testImage(t)
			if err := os.WriteFile(dest, original, 0644); err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}
				switch mode {
				case "http":
					resp.StatusCode = 403
				case "html":
					resp.Body = io.NopCloser(strings.NewReader("<html>Access denied</html>"))
				case "interrupted":
					resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(original[:10]), interruptedReader{}))
				}
				return resp, nil
			})}
			if err := downloadPhoto(context.Background(), client, "https://example.com/photo.png", dest); err == nil {
				t.Fatal("failed download must return error")
			}
			got, err := os.ReadFile(dest)
			if err != nil || !bytes.Equal(got, original) {
				t.Fatal("failed download replaced existing image")
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 1 {
				t.Fatal("temporary download was not cleaned up")
			}
		})
	}
}
