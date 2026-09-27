# Woodworking site for jonkeane

Project listings use hugo-theme-gallery. Project photos come from Cloudflare R2
manifests published by [publish-to-r2](https://github.com/jonkeane/publish-to-r2).
Hugo generates static project grids and one page per photo. Gallery and project
card images are served from the R2 public domain.
The grid and photo viewer layout are unchanged.

Project galleries use CSS flexbox and image dimensions from the manifest to form
full-width rows of equally tall, uncropped photos with a 10px gap. Row heights
adapt to the viewport; a short final row stays left-aligned. The layout works
with or without JavaScript. Photo pages use Swiper for swipe navigation, pinch
zoom, and panning, with a plain image as a no-JavaScript fallback. Double-click
or double-tap the viewer to open an image-only viewport; press Escape or repeat
the gesture to restore the page controls.

## Publish and import

Requires Go 1.26+, Hugo extended 0.152.2+, and Node 20+. The fixture tests
also use Python 3.

1. Publish each woodworking collection with publish-to-r2 from Lightroom. In
   **Edit Collection**, use **Copy Gallery ID** after the first successful publish.
2. Add `r2_gallery_id: "GALLERY_ID"` to the corresponding
   `content/<project>/_index.md`. The gallery ID is distinct from the old Flickr
   album ID. Tag one photo in each Lightroom collection `gallery-cover` and
   republish; it supplies the homepage card and project social preview.
3. Run the importer before building. It defaults to the public image domain
   used by photo-site (`https://jonkeane.link`). Set `R2_PUBLIC_BASE_URL` if
   these collections use a different public domain:

```sh
hugo mod vendor
npm ci
# Optional: export R2_PUBLIC_BASE_URL=https://YOUR_IMAGE_DOMAIN
npm run import:r2
npm run build
```

`npm run import:r2` runs this direct `publish-to-r2` command for the site's
`content/<project>/_index.md` layout:

```sh
R2_PUBLIC_BASE_URL=https://jonkeane.link \
  go run github.com/jonkeane/publish-to-r2/uploader/cmd/r2import@latest \
  -content-dir content
```

The importer fetches `galleries/GALLERY_ID/current.json` for each configured
project, validates it with `r2import`, and writes
a snapshot to `data/r2/galleries/<project>.json`. It does not download photographs
or need R2 API keys. Manifests are ignored by Git and refreshed on each deploy.
Photos tagged `nogallery` or `gallery-cover` are excluded from project grids and
photo pages. Exactly one `gallery-cover` photo is required per project.
The separate homepage social-preview image at `content/B0000089.jpg` remains local.
Missing or mismatched manifests fail the Hugo build for configured projects.

All seven projects now have R2 gallery IDs.

Run `npm test` to build an isolated fixture site and check gallery pages,
photo URLs, navigation, tag filtering, and missing-manifest failures. The
fixture does not modify real imported manifests.

## Deployment

GitHub Actions imports manifests, builds, and deploys main to Netlify. PRs use
preview deployments; fork PRs run fixture tests without credentials. A manual
run can build only, deploy a preview, or deploy production. Set these Actions
secrets:

- `NETLIFY_AUTH_TOKEN` and this woodworking site's `NETLIFY_SITE_ID`.

Disable Netlify's automatic Git builds when using Actions deployment to avoid
duplicate deployments.
Set an `R2_PUBLIC_BASE_URL` repository variable or secret in GitHub Actions,
and the same environment variable in Netlify, only if the woodworking
collections use a public domain other than `https://jonkeane.link`.
