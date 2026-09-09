# Woodworking site for jonkeane

Project listings use hugo-theme-gallery. A Go importer (ported from
`jonkeane/photo-site`) fetches Flickr metadata at build time. Hugo generates
static project grids and one page per photo; downloaded images deploy with the site.
Gallery browsing does not require JavaScript or expose API credentials.
Project galleries use CSS flexbox and the local thumbnail dimensions to form
full-width rows of equally tall, uncropped photos with a 10px gap. Row heights adapt to the viewport; a short final row stays left-aligned.
The layout is the same with JavaScript enabled or disabled. JavaScript supports
the site's menu, while photo pages use Swiper for swipe navigation, pinch zoom,
and panning. Explicit prefetching of adjacent pages and images is disabled. Each
photo page keeps its ordinary image as a no-JavaScript fallback.
Double-clicking or double-tapping the viewer opens an image-only viewport; press
Escape, use the visible Close button, or repeat the gesture to restore the page
controls and reset image zoom. Desktop trackpad pinches and Control-scroll over
the viewer zoom the image; Escape also resets zoom in the normal view. With the
viewer focused, +/− zoom and 0 resets. Mouse dragging and horizontal trackpad
gestures navigate on desktop. The thumbnail
strip centers the current photo, including after resizing or leaving image-only view.

## Development

Requires Go 1.23+, Hugo extended 0.152.2+, and Node 20+.

```sh
hugo mod vendor
npm ci
export FLICKR_API_KEY=your-key
npm run import:flickr
npm run dev
```

`npm run build` builds from existing metadata and downloaded images. `npm test`
tests the importer and builds an isolated fixture site, including navigation and
tag filtering checks.
Tailwind scans the vendored theme layouts as well as this site's overrides.

## Flickr

Projects live in `content/<project>/_index.md` with `flickr_album: "ID"`.
Keep the local cover image beside that file for the project listing.
The importer discovers these projects and writes `data/flickr/photosets/<project>.json`.
It also downloads each photo's thumbnail (preferring `_z`) and large image
(preferring `_k`, with smaller sizes or the original as fallbacks) into `assets/flickr/`.
Hugo publishes these files with the site and uses local URLs for grids, the viewer,
and social previews. Metadata and downloaded images are ignored by Git; import
before the first build and whenever photos change. To download images from existing
metadata without API credentials, run `go run ./cmd/flickrimport -fromCache`.
Images are keyed by their source URL and reused across imports and albums. A changed
URL or missing/corrupt file triggers a download; metadata-only edits do not.
Incomplete downloads are never saved as cached images.
Cached EXIF and tags are reused for unchanged photos. Album order is preserved.
Photos tagged `nogallery` are excluded from both grids and generated pages.
Missing or mismatched metadata, or missing local gallery images, stops the build.

Public albums only need `FLICKR_API_KEY`. For OAuth, set `FLICKR_CONSUMER_KEY` and
`FLICKR_CONSUMER_SECRET`, then run `go run ./cmd/flickrimport -initOAuth` once.
The token is stored in `~/.flickr_oauth_token`; use `-tokenFile` to override.
For a single project, use `go run ./cmd/flickrimport -onlySet ALBUM_ID`.
The optional `-downloadAssets -assetsSetID ALBUM_ID` mode from the photo site is
available, but existing local project covers do not need it.

## Deployment

The GitHub workflow builds and deploys main to Netlify, creates PR previews, and
refreshes/deploys Flickr photos weekly. Manual runs support build-only, preview,
or production and can clear the metadata and image cache.
GitHub Actions restores and saves `data/flickr/` and `assets/flickr/` together,
including saving a fresh cache after a manual cache reset. Fork PRs run tests with
fixtures without accessing credentials or deploying.
Each PR preview is linked from GitHub's deployment status and from one
automatically updated PR comment.

Configure these repository Actions secrets:

- `FLICKR_API_KEY` for public albums, **or** `FLICKR_CONSUMER_KEY`,
  `FLICKR_CONSUMER_SECRET`, and `FLICKR_OAUTH_TOKEN` (the token file's JSON).
- `NETLIFY_AUTH_TOKEN` and this woodworking site's `NETLIFY_SITE_ID`.

Disable Netlify's automatic Git builds when using the Actions deployment to avoid
duplicate deployments. Alternatively, Netlify's build command imports and builds
directly when `FLICKR_API_KEY` is set in the Netlify environment. The old
`HUGOxPARAMSxNANOGxflickr_apikey` setting is no longer used.
