package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/dghubble/oauth1"
)

// Ported from github.com/jonkeane/photo-site (MIT).
// Minimal, dependency-light Flickr importer that:
// - Scans content/* for gallery pages with a flickr_album param
// - Calls flickr.photosets.getPhotos with rich `extras` once per photoset (with pagination)
// - Fetches EXIF and tags for new or updated photos
// - Writes raw JSON responses under data/flickr/photosets/{slug}.json
// - Does NOT generate per-image markdown stubs.
// - Downloads gallery thumbnails and full-size images into assets/flickr.

// Environment / flags
//   FLICKR_API_KEY env var or -apikey flag
//   -onlySet <photoset_id> optional to limit run
//   (always fetch exif per photo)
//   -timeout http timeout (default 20s)

const (
	baseAPI = "https://api.flickr.com/services/rest/"
)

// doRequestWithBackoff performs an HTTP request with exponential backoff on 429 and transient errors.
func doRequestWithBackoff(client *http.Client, req *http.Request) (*http.Response, error) {
	operation := func() (*http.Response, error) {
		r, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		// Retry on 429 Too Many Requests
		if r.StatusCode == http.StatusTooManyRequests {
			r.Body.Close()
			return nil, fmt.Errorf("http 429: too many requests")
		}
		// Retry on 5xx errors (except 501 Not Implemented)
		if r.StatusCode >= 500 && r.StatusCode != http.StatusNotImplemented {
			r.Body.Close()
			return nil, fmt.Errorf("http %d: server error", r.StatusCode)
		}
		return r, nil
	}

	b := backoff.NewExponentialBackOff()
	ctx := req.Context()
	resp, err := backoff.Retry(ctx, operation, backoff.WithBackOff(b), backoff.WithMaxElapsedTime(30*time.Second))
	return resp, err
}

type PhotosetsGetPhotosResp struct {
	Photoset struct {
		ID      string      `json:"id"`
		Owner   string      `json:"owner"`
		Photo   []Photo     `json:"photo"`
		Page    json.Number `json:"page"`
		Pages   json.Number `json:"pages"`
		PerPage json.Number `json:"perpage"`
		Total   json.Number `json:"total"`
	} `json:"photoset"`
	Stat    string                     `json:"stat"`
	Message string                     `json:"message,omitempty"`
	Code    int                        `json:"code,omitempty"`
	Exif    map[string]json.RawMessage `json:"exif,omitempty"`
}

type Photo struct {
	ThumbnailAsset string `json:"thumbnail_asset,omitempty"`
	FullAsset      string `json:"full_asset,omitempty"`
	ID             string `json:"id"`
	Title          string `json:"title"`
	Description    struct {
		Content string `json:"_content"`
	} `json:"description"`
	Tags       []string `json:"tags"`
	DateUpload string   `json:"dateupload"`
	DateTaken  string   `json:"datetaken"`
	OwnerName  string   `json:"ownername"`
	License    string   `json:"license"`
	PathAlias  string   `json:"pathalias"`
	URLSq      string   `json:"url_sq"`
	URLT       string   `json:"url_t"`
	URLS       string   `json:"url_s"`
	URLN       string   `json:"url_n"`
	URLM       string   `json:"url_m"`
	URLZ       string   `json:"url_z"`
	URLC       string   `json:"url_c"`
	URLL       string   `json:"url_l"`
	URLH       string   `json:"url_h"`
	URLK       string   `json:"url_k"`
	URLO       string   `json:"url_o"`
	// Flickr dimensions may be JSON numbers or strings. Preserve both forms
	// so Hugo can lay out thumbnails before the browser loads any images.
	WidthZ         json.Number      `json:"width_z,omitempty"`
	HeightZ        json.Number      `json:"height_z,omitempty"`
	WidthC         json.Number      `json:"width_c,omitempty"`
	HeightC        json.Number      `json:"height_c,omitempty"`
	WidthM         json.Number      `json:"width_m,omitempty"`
	HeightM        json.Number      `json:"height_m,omitempty"`
	WidthL         json.Number      `json:"width_l,omitempty"`
	HeightL        json.Number      `json:"height_l,omitempty"`
	WidthH         json.Number      `json:"width_h,omitempty"`
	HeightH        json.Number      `json:"height_h,omitempty"`
	WidthK         json.Number      `json:"width_k,omitempty"`
	HeightK        json.Number      `json:"height_k,omitempty"`
	WidthO         json.Number      `json:"width_o,omitempty"`
	HeightO        json.Number      `json:"height_o,omitempty"`
	LastUpdate     string           `json:"lastupdate"`
	OriginalFormat string           `json:"originalformat"`
	Exif           *PhotoExifFields `json:"exif,omitempty"`
}

type PhotoExifFields struct {
	Model             string `json:"model,omitempty"`
	Lens              string `json:"lens,omitempty"`
	Flash             string `json:"flash,omitempty"`
	FocalLength       string `json:"focallength,omitempty"`
	FStop             string `json:"fstop,omitempty"`
	Exposure          string `json:"exposure,omitempty"`
	ISO               string `json:"iso,omitempty"`
	Time              string `json:"time,omitempty"`
	Location          string `json:"location,omitempty"`
	PreservedFileName string `json:"preservedfilename,omitempty"`
}

type PhotoExifResp struct {
	Photo json.RawMessage `json:"photo"`
	Stat  string          `json:"stat"`
}

type Config struct {
	APIKey         string
	ConsumerKey    string
	ConsumerSecret string
	HTTPTimeout    time.Duration
	OnlySet        string
	ForceSlug      string
	ForceSetID     string
	DownloadAssets bool
	AssetsSetID    string
	OAuthTokenFile string
	InitOAuth      bool
	FromCache      bool
}

func main() {
	cfg := loadConfig()

	// OAuth initialization mode
	if cfg.InitOAuth {
		if cfg.ConsumerKey == "" || cfg.ConsumerSecret == "" {
			fmt.Fprintln(os.Stderr, "Consumer key and secret required. Set FLICKR_CONSUMER_KEY and FLICKR_CONSUMER_SECRET env vars or use -consumerKey and -consumerSecret flags")
			os.Exit(1)
		}
		if err := initOAuthFlow(cfg); err != nil {
			fatal(err)
		}
		return
	}

	ctx := context.Background()

	// Create HTTP client (with OAuth if token file exists)
	imageClient := &http.Client{Timeout: cfg.HTTPTimeout}
	client := imageClient
	var err error
	if cfg.FromCache && cfg.DownloadAssets {
		fatal(fmt.Errorf("-fromCache cannot be combined with -downloadAssets"))
	}
	if !cfg.FromCache {
		client, err = createHTTPClient(cfg)
		if err != nil {
			fatal(err)
		}
	}

	// Download assets mode: fetch specific album and download originals
	if cfg.DownloadAssets {
		if cfg.AssetsSetID == "" {
			fatal(fmt.Errorf("-assetsSetID is required with -downloadAssets"))
		}
		if err := downloadAssets(ctx, client, cfg); err != nil {
			fatal(err)
		}
		fmt.Println("Assets download complete.")
		return
	}

	// Explicit sets use the same EXIF/tag pipeline as discovered projects.
	var galleries []Gallery
	if cfg.ForceSetID != "" {
		slug := cfg.ForceSlug
		if slug == "" {
			slug = cfg.ForceSetID
		}
		galleries = []Gallery{{Slug: slug, PhotosetID: cfg.ForceSetID}}
	} else {
		galleries, err = findGalleries("content")
		if err != nil {
			fatal(err)
		}
	}
	if len(galleries) == 0 {
		fmt.Println("No galleries found.")
		return
	}

	for _, g := range galleries {
		if cfg.OnlySet != "" && g.PhotosetID != cfg.OnlySet {
			continue
		}
		fmt.Printf("Ingesting photoset %s (%s)\n", g.PhotosetID, g.Slug)

		// Fetch all photos in set with extras
		var ps *PhotosetsGetPhotosResp
		jsonPath := filepath.Join("data", "flickr", "photosets", g.Slug+".json")
		if cfg.FromCache {
			var b []byte
			b, err = os.ReadFile(jsonPath)
			if err == nil {
				ps = &PhotosetsGetPhotosResp{}
				err = json.Unmarshal(b, ps)
			}
			if err == nil && (ps.Stat != "ok" || ps.Photoset.ID != g.PhotosetID) {
				err = fmt.Errorf("invalid cached metadata for %s", g.Slug)
			}
		} else {
			ps, err = fetchPhotosetAll(ctx, client, cfg.APIKey, g.PhotosetID)
		}
		if err != nil {
			fatal(err)
		}

		// Detector: check for existing JSON and compare lastupdate
		var oldPhotos map[string]string // photoID -> lastupdate
		var oldExif map[string]*PhotoExifFields
		var oldTags map[string][]string // photoID -> tags
		oldPhotos = make(map[string]string)
		oldExif = make(map[string]*PhotoExifFields)
		oldTags = make(map[string][]string)
		if _, err := os.Stat(jsonPath); err == nil {
			// JSON exists, load it
			b, err := os.ReadFile(jsonPath)
			if err == nil {
				var oldData PhotosetsGetPhotosResp
				if err := json.Unmarshal(b, &oldData); err == nil {
					for _, p := range oldData.Photoset.Photo {
						oldPhotos[p.ID] = p.LastUpdate
						if p.Exif != nil {
							oldExif[p.ID] = p.Exif
						}
						oldTags[p.ID] = p.Tags
					}
				}
			}
		}

		// Only fetch EXIF and tags for new/updated photos
		for i := range ps.Photoset.Photo {
			if cfg.FromCache {
				break
			}
			photo := &ps.Photoset.Photo[i]
			oldDate, exists := oldPhotos[photo.ID]
			if exists && oldDate == photo.LastUpdate && oldExif[photo.ID] != nil {
				// No change, reuse old EXIF and tags
				photo.Exif = oldExif[photo.ID]
				photo.Tags = oldTags[photo.ID]
				continue
			}
			fmt.Fprintf(os.Stderr, "Fetching EXIF and tags for photo %s\n", photo.ID)
			// Fetch EXIF
			exifRaw, err := fetchPhotoExif(ctx, client, cfg.APIKey, photo.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to fetch exif for photo %s: %v\n", photo.ID, err)
			}
			exifFields := extractExifFields(exifRaw)
			photo.Exif = exifFields

			// Fetch accurate tags
			infoRaw, err := fetchPhotoInfo(ctx, client, cfg.APIKey, photo.ID)
			if err != nil {
				fatal(fmt.Errorf("failed to fetch tags for photo %s: %w", photo.ID, err))
			} else {
				photo.Tags = extractTagsFromInfo(infoRaw)
			}
			time.Sleep(240 * time.Millisecond) // be polite
		}

		// Static image requests must not carry the API client's OAuth credentials.
		if err := downloadGalleryImages(ctx, imageClient, "assets", ps); err != nil {
			fatal(fmt.Errorf("gallery %s: %w", g.Slug, err))
		}

		// Persist metadata only once both image sizes are available locally.
		if err := writePhotosetJSON(g.Slug, ps); err != nil {
			fatal(err)
		}
	}

	fmt.Println("Done.")
}

func loadConfig() Config {
	var cfg Config
	apikey := os.Getenv("FLICKR_API_KEY")
	consumerKey := os.Getenv("FLICKR_CONSUMER_KEY")
	consumerSecret := os.Getenv("FLICKR_CONSUMER_SECRET")
	homeDir, _ := os.UserHomeDir()
	defaultTokenFile := filepath.Join(homeDir, ".flickr_oauth_token")

	flag.StringVar(&apikey, "apikey", apikey, "Flickr API key (legacy, prefer OAuth)")
	flag.StringVar(&consumerKey, "consumerKey", consumerKey, "Flickr OAuth consumer key")
	flag.StringVar(&consumerSecret, "consumerSecret", consumerSecret, "Flickr OAuth consumer secret")
	flag.DurationVar(&cfg.HTTPTimeout, "timeout", 20*time.Second, "HTTP timeout")
	flag.StringVar(&cfg.OnlySet, "onlySet", "", "Limit to a single photoset id")
	flag.StringVar(&cfg.ForceSlug, "slug", "", "Explicit slug for writing data (bypass discovery)")
	flag.StringVar(&cfg.ForceSetID, "set", "", "Explicit photoset id (bypass discovery)")
	flag.BoolVar(&cfg.DownloadAssets, "downloadAssets", false, "Download original photos from assets album to assets/ folder")
	flag.StringVar(&cfg.AssetsSetID, "assetsSetID", "", "Photoset ID for assets download (required with -downloadAssets)")
	flag.StringVar(&cfg.OAuthTokenFile, "tokenFile", defaultTokenFile, "Path to OAuth token file")
	flag.BoolVar(&cfg.InitOAuth, "initOAuth", false, "Initialize OAuth flow to get access token")
	flag.BoolVar(&cfg.FromCache, "fromCache", false, "Download gallery images using existing metadata without calling the Flickr API")
	flag.Parse()

	cfg.APIKey = apikey
	cfg.ConsumerKey = consumerKey
	cfg.ConsumerSecret = consumerSecret
	return cfg
}

type Gallery struct {
	Slug       string
	PhotosetID string
}

var fmSetRe = regexp.MustCompile(`(?m)^flickr_album:\s*"?([0-9]+)"?`)

func findGalleries(root string) ([]Gallery, error) {
	var out []Gallery
	// Strategy:
	// 1. Prefer _index.md inside a directory (content/{slug}/_index.md)
	// 2. Fallback to legacy flat file (content/{slug}.md)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		// Skip top-level index or non-md files
		if e.IsDir() {
			// Look for _index.md within directory
			idx := filepath.Join(root, name, "_index.md")
			if b, err := os.ReadFile(idx); err == nil {
				if m := fmSetRe.FindStringSubmatch(string(b)); len(m) == 2 {
					out = append(out, Gallery{Slug: name, PhotosetID: m[1]})
					continue
				}
			}
		}
		// Legacy: flat markdown file named {slug}.md
		if strings.HasSuffix(name, ".md") && !strings.HasPrefix(name, "_") {
			path := filepath.Join(root, name)
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if m := fmSetRe.FindStringSubmatch(string(b)); len(m) == 2 {
				slug := strings.TrimSuffix(name, filepath.Ext(name))
				out = append(out, Gallery{Slug: slug, PhotosetID: m[1]})
			}
		}
	}
	return out, nil
}

func fetchPhotosetAll(ctx context.Context, client *http.Client, apiKey, setID string) (*PhotosetsGetPhotosResp, error) {
	perPage := 500
	page := 1
	var all []Photo
	var header PhotosetsGetPhotosResp
	for {
		resp, err := fetchPhotosetPage(ctx, client, apiKey, setID, page, perPage)
		if err != nil {
			return nil, err
		}
		if resp.Stat != "ok" {
			return nil, fmt.Errorf("flickr error: code=%d message=%s", resp.Code, resp.Message)
		}
		if page == 1 {
			header = *resp
		}
		all = append(all, resp.Photoset.Photo...)
		pagesInt, err := resp.Photoset.Pages.Int64()
		if err != nil {
			return nil, fmt.Errorf("invalid pages value: %v", err)
		}
		if int64(page) >= pagesInt || len(resp.Photoset.Photo) == 0 {
			break
		}
		page++
		time.Sleep(240 * time.Millisecond) // be polite
	}
	header.Photoset.Photo = all
	return &header, nil
}

func fetchPhotosetPage(ctx context.Context, client *http.Client, apiKey, setID string, page, perPage int) (*PhotosetsGetPhotosResp, error) {
	q := url.Values{}
	q.Set("method", "flickr.photosets.getPhotos")
	q.Set("api_key", apiKey)
	q.Set("photoset_id", setID)
	q.Set("format", "json")
	q.Set("nojsoncallback", "1")
	q.Set("page", fmt.Sprintf("%d", page))
	q.Set("per_page", fmt.Sprintf("%d", perPage))
	q.Set("extras", strings.Join([]string{
		"description", "date_upload", "date_taken", "owner_name", "license", "path_alias", "last_update",
		"url_sq", "url_t", "url_s", "url_n", "url_m", "url_z", "url_c", "url_l", "url_h", "url_k", "url_o", "original_format",
	}, ","))

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseAPI+"?"+q.Encode(), nil)
	res, err := doRequestWithBackoff(client, req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("http %d: %s", res.StatusCode, string(b))
	}
	var data PhotosetsGetPhotosResp
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

func writePhotosetJSON(slug string, ps *PhotosetsGetPhotosResp) error {
	dir := filepath.Join("data", "flickr", "photosets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, slug+".json")
	b, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Fetch EXIF data for a photo
func fetchPhotoExif(ctx context.Context, client *http.Client, apiKey, photoID string) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("method", "flickr.photos.getExif")
	q.Set("api_key", apiKey)
	q.Set("photo_id", photoID)
	q.Set("format", "json")
	q.Set("nojsoncallback", "1")

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseAPI+"?"+q.Encode(), nil)
	res, err := doRequestWithBackoff(client, req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("http %d: %s", res.StatusCode, string(b))
	}
	var data PhotoExifResp
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&data); err != nil {
		return nil, err
	}
	if data.Stat != "ok" {
		return nil, fmt.Errorf("flickr error: %s", data.Stat)
	}
	return data.Photo, nil
}

// Fetch photo info (for accurate tags)
func fetchPhotoInfo(ctx context.Context, client *http.Client, apiKey, photoID string) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("method", "flickr.photos.getInfo")
	q.Set("api_key", apiKey)
	q.Set("photo_id", photoID)
	q.Set("format", "json")
	q.Set("nojsoncallback", "1")

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseAPI+"?"+q.Encode(), nil)
	res, err := doRequestWithBackoff(client, req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("http %d: %s", res.StatusCode, string(b))
	}
	var data struct {
		Photo json.RawMessage `json:"photo"`
		Stat  string          `json:"stat"`
	}
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&data); err != nil {
		return nil, err
	}
	if data.Stat != "ok" {
		return nil, fmt.Errorf("flickr error: %s", data.Stat)
	}
	return data.Photo, nil
}

// Extract tags from photo info JSON
func extractTagsFromInfo(raw json.RawMessage) []string {
	var parsed struct {
		Tags struct {
			Tag []struct {
				Raw string `json:"raw"`
			} `json:"tag"`
		} `json:"tags"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	var tags []string
	for _, t := range parsed.Tags.Tag {
		tags = append(tags, t.Raw)
	}
	return tags
}

// Extract only the requested EXIF fields from the raw JSON
func extractExifFields(raw json.RawMessage) *PhotoExifFields {
	var parsed struct {
		Camera string `json:"camera"`
		Exif   []struct {
			Tag string `json:"tag"`
			Raw struct {
				Content string `json:"_content"`
			} `json:"raw"`
			Cleaned struct {
				Content string `json:"_content"`
			} `json:"clean"`
		} `json:"exif"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	fields := &PhotoExifFields{}
	if parsed.Camera != "" {
		fields.Model = parsed.Camera
	}
	for _, e := range parsed.Exif {
		raw_val := e.Raw.Content
		cleaned_val := e.Cleaned.Content
		if cleaned_val == "" {
			cleaned_val = raw_val
		}
		switch e.Tag {
		case "LensModel":
			fields.Lens = cleaned_val
		case "Flash":
			fields.Flash = cleaned_val
		case "FocalLength":
			fields.FocalLength = cleaned_val
		case "FNumber":
			fields.FStop = cleaned_val
		case "ExposureTime":
			fields.Exposure = raw_val
		case "ISO":
			fields.ISO = cleaned_val
		case "DateTimeOriginal":
			fields.Time = cleaned_val
		case "LOLcation":
			fields.Location = cleaned_val
		case "PreservedFileName":
			fields.PreservedFileName = cleaned_val
		}
	}
	return fields
}

func safeTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Untitled"
	}
	return s
}

// OAuth token storage
type OAuthToken struct {
	Token       string `json:"token"`
	TokenSecret string `json:"token_secret"`
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
}

// initOAuthFlow performs the OAuth 1.0a flow to get an access token
func initOAuthFlow(cfg Config) error {
	config := oauth1.Config{
		ConsumerKey:    cfg.ConsumerKey,
		ConsumerSecret: cfg.ConsumerSecret,
		CallbackURL:    "oob", // out-of-band for command-line apps
		Endpoint: oauth1.Endpoint{
			RequestTokenURL: "https://www.flickr.com/services/oauth/request_token",
			AuthorizeURL:    "https://www.flickr.com/services/oauth/authorize",
			AccessTokenURL:  "https://www.flickr.com/services/oauth/access_token",
		},
	}

	// Step 1: Get request token
	requestToken, requestSecret, err := config.RequestToken()
	if err != nil {
		return fmt.Errorf("failed to get request token: %w", err)
	}

	// Step 2: Get authorization URL
	authURL, err := config.AuthorizationURL(requestToken)
	if err != nil {
		return fmt.Errorf("failed to get authorization URL: %w", err)
	}

	// Add perms parameter for read access
	authURLWithPerms := authURL.String() + "&perms=read"

	fmt.Println("\n=== Flickr OAuth Authorization ===")
	fmt.Println("\n1. Visit this URL in your browser:")
	fmt.Printf("\n   %s\n\n", authURLWithPerms)
	fmt.Println("2. Authorize the application")
	fmt.Println("3. Copy the verification code and paste it here")
	fmt.Print("\nVerification code: ")

	// Read verifier from user
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	verifier := strings.TrimSpace(scanner.Text())
	if verifier == "" {
		return fmt.Errorf("no verification code provided")
	}

	// Step 3: Exchange for access token
	accessToken, accessSecret, err := config.AccessToken(requestToken, requestSecret, verifier)
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	// Get user info
	token := oauth1.NewToken(accessToken, accessSecret)
	httpClient := config.Client(context.Background(), token)

	resp, err := httpClient.Get("https://api.flickr.com/services/rest/?method=flickr.test.login&format=json&nojsoncallback=1")
	if err != nil {
		return fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	var userInfo struct {
		User struct {
			ID       string `json:"id"`
			Username struct {
				Content string `json:"_content"`
			} `json:"username"`
		} `json:"user"`
		Stat string `json:"stat"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return fmt.Errorf("failed to decode user info: %w", err)
	}

	if userInfo.Stat != "ok" {
		return fmt.Errorf("failed to verify token")
	}

	// Save token to file
	tokenData := OAuthToken{
		Token:       accessToken,
		TokenSecret: accessSecret,
		UserID:      userInfo.User.ID,
		Username:    userInfo.User.Username.Content,
	}

	tokenJSON, err := json.MarshalIndent(tokenData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	if err := os.WriteFile(cfg.OAuthTokenFile, tokenJSON, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	fmt.Printf("\n✓ Successfully authenticated as %s (ID: %s)\n", tokenData.Username, tokenData.UserID)
	fmt.Printf("✓ Token saved to: %s\n\n", cfg.OAuthTokenFile)
	fmt.Println("You can now use the tool with OAuth authentication.")

	return nil
}

// createHTTPClient creates an HTTP client with OAuth if token file exists
func createHTTPClient(cfg Config) (*http.Client, error) {
	// An API-key-only invocation should not require unrelated local OAuth credentials.
	if _, err := os.Stat(cfg.OAuthTokenFile); err == nil && cfg.ConsumerKey != "" && cfg.ConsumerSecret != "" {
		// Load OAuth token
		tokenData, err := os.ReadFile(cfg.OAuthTokenFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read token file: %w", err)
		}

		var token OAuthToken
		if err := json.Unmarshal(tokenData, &token); err != nil {
			return nil, fmt.Errorf("failed to parse token file: %w", err)
		}

		if cfg.ConsumerKey == "" || cfg.ConsumerSecret == "" {
			return nil, fmt.Errorf("consumer key and secret required for OAuth. Set FLICKR_CONSUMER_KEY and FLICKR_CONSUMER_SECRET")
		}

		// Create OAuth client
		config := oauth1.Config{
			ConsumerKey:    cfg.ConsumerKey,
			ConsumerSecret: cfg.ConsumerSecret,
		}

		oauthToken := oauth1.NewToken(token.Token, token.TokenSecret)
		httpClient := config.Client(context.Background(), oauthToken)
		httpClient.Timeout = cfg.HTTPTimeout

		fmt.Printf("Using OAuth authentication as %s\n", token.Username)
		return httpClient, nil
	}

	// Fall back to regular HTTP client (for API key usage)
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("set FLICKR_API_KEY or configure OAuth with -initOAuth")
	}
	return &http.Client{Timeout: cfg.HTTPTimeout}, nil
}

func fatal(err error) {
	if err == nil {
		return
	}
	var e *url.Error
	if errors.As(err, &e) {
		fmt.Fprintf(os.Stderr, "Error: %v\n", e)
	} else {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(1)
}

// downloadAssets fetches photos from the assets album and downloads them to assets/ folder
func downloadAssets(ctx context.Context, client *http.Client, cfg Config) error {
	fmt.Printf("Fetching assets from photoset %s\n", cfg.AssetsSetID)

	// Fetch all photos in the assets set
	ps, err := fetchPhotosetAll(ctx, client, cfg.APIKey, cfg.AssetsSetID)
	if err != nil {
		return fmt.Errorf("failed to fetch photoset: %w", err)
	}

	if len(ps.Photoset.Photo) == 0 {
		return fmt.Errorf("no photos found in photoset")
	}

	// Create base assets directories
	bgDir := "assets/bg"
	galleryDir := "assets/gallery"
	if err := os.MkdirAll(bgDir, 0o755); err != nil {
		return fmt.Errorf("failed to create bg directory: %w", err)
	}
	if err := os.MkdirAll(galleryDir, 0o755); err != nil {
		return fmt.Errorf("failed to create gallery directory: %w", err)
	}

	// Process each photo
	for i, photo := range ps.Photoset.Photo {
		fmt.Printf("[%d/%d] Processing photo %s\n", i+1, len(ps.Photoset.Photo), photo.ID)

		// Determine the filename from title and original format
		filename := ""
		if photo.Title != "" {
			filename = photo.Title
			// Append extension from originalformat field
			if photo.OriginalFormat != "" {
				filename = filename + "." + photo.OriginalFormat
			} else {
				// Fallback to .jpg if no format specified
				filename = filename + ".jpg"
			}
		} else {
			// Fallback to photo ID with extension
			ext := "jpg"
			if photo.OriginalFormat != "" {
				ext = photo.OriginalFormat
			}
			filename = photo.ID + "." + ext
			fmt.Fprintf(os.Stderr, "Warning: no title for photo %s, using %s\n", photo.ID, filename)
		}

		// Determine target directory based on title
		// If the photo title is "background", put it in bg/, otherwise gallery/
		targetDir := galleryDir
		titleLower := strings.ToLower(strings.TrimSpace(photo.Title))
		if titleLower == "background" {
			targetDir = bgDir
		}

		targetPath := filepath.Join(targetDir, filename)

		// Check if file already exists and is up-to-date
		if fileInfo, err := os.Stat(targetPath); err == nil {
			// File exists, compare timestamps
			// Parse Flickr's lastupdate (Unix timestamp as string)
			if photo.LastUpdate != "" {
				flickrTimestamp, err := strconv.ParseInt(photo.LastUpdate, 10, 64)
				if err == nil {
					flickrModTime := time.Unix(flickrTimestamp, 0)
					fileModTime := fileInfo.ModTime()

					// If file is newer than or equal to Flickr's lastupdate, skip
					if !fileModTime.Before(flickrModTime) {
						fmt.Printf("  Skipping %s (up-to-date)\n", filename)
						continue
					}
					fmt.Printf("  Re-downloading %s (Flickr updated: %s, local: %s)\n",
						filename, flickrModTime.Format(time.RFC3339), fileModTime.Format(time.RFC3339))
				}
			} else {
				// No lastupdate available, skip if file exists
				fmt.Printf("  Skipping %s (already exists, no timestamp to compare)\n", filename)
				continue
			}
		}

		// Download the original photo
		if photo.URLO == "" {
			fmt.Fprintf(os.Stderr, "Warning: no original URL for photo %s\n", photo.ID)
			continue
		}

		if err := downloadPhoto(ctx, client, photo.URLO, targetPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to download photo %s: %v\n", photo.ID, err)
			continue
		}

		fmt.Printf("  Downloaded %s\n", filename)
		time.Sleep(240 * time.Millisecond) // be polite
	}

	return nil
}

func firstURL(urls ...string) string {
	for _, u := range urls {
		if u != "" {
			return u
		}
	}
	return ""
}

func downloadGalleryImages(ctx context.Context, client *http.Client, assetsDir string, ps *PhotosetsGetPhotosResp) error {
	for i := range ps.Photoset.Photo {
		p := &ps.Photoset.Photo[i]
		for _, variant := range []struct {
			name string
			url  string
			path *string
		}{
			{"thumbnail", firstURL(p.URLZ, p.URLC, p.URLM, p.URLL, p.URLH, p.URLK, p.URLO), &p.ThumbnailAsset},
			{"full", firstURL(p.URLK, p.URLH, p.URLL, p.URLC, p.URLZ, p.URLO), &p.FullAsset},
		} {
			u, err := url.Parse(variant.url)
			if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
				return fmt.Errorf("photo %s: missing or invalid %s URL", p.ID, variant.name)
			}
			// Flickr replacement images have new URLs. Key by the complete source
			// URL so metadata edits don't trigger downloads and albums share files.
			ext := strings.ToLower(filepath.Ext(u.Path))
			switch ext {
			case ".jpg", ".jpeg", ".png", ".gif":
			default:
				return fmt.Errorf("photo %s: unsupported image extension %q", p.ID, ext)
			}
			asset := fmt.Sprintf("flickr/%x%s", sha256.Sum256([]byte(variant.url)), ext)
			dest := filepath.Join(assetsDir, filepath.FromSlash(asset))
			if err := validateImage(dest); err != nil {
				if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
					return err
				}
				fmt.Printf("Downloading photo %s (%s)\n", p.ID, variant.name)
				if err := downloadPhoto(ctx, client, variant.url, dest); err != nil {
					return fmt.Errorf("photo %s (%s): %w", p.ID, variant.name, err)
				}
			}
			*variant.path = asset
		}
	}
	return nil
}

func validateImage(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, _, err = image.Decode(f)
	return err
}

// downloadPhoto publishes only complete, valid images, leaving existing files
// intact on failure and ensuring interrupted downloads cannot poison the cache.
func downloadPhoto(ctx context.Context, client *http.Client, photoURL, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, photoURL, nil)
	if err != nil {
		return err
	}
	// Images are already compressed. Flickr's CDN can return 502 when Go's
	// default transport requests gzip, so request the original image bytes.
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "woodworking-flickrimport")
	req.Header.Set("Accept", "*/*")

	resp, err := doRequestWithBackoff(client, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}

	out, err := os.CreateTemp(filepath.Dir(destPath), ".flickr-download-*")
	if err != nil {
		return err
	}
	defer out.Close()
	defer os.Remove(out.Name())

	// Write the body to file
	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := validateImage(out.Name()); err != nil {
		return fmt.Errorf("invalid downloaded image: %w", err)
	}
	if err := os.Chmod(out.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(out.Name(), destPath)
}
