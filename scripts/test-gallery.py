#!/usr/bin/env python3
"""Build an isolated R2 fixture site without touching imported manifests."""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
BASE = "https://images.example.com"

with tempfile.TemporaryDirectory(prefix="woodworking-gallery-") as tmp:
    site = Path(tmp)
    for name in ("hugo.toml", "go.mod", "go.sum", "package.json", "postcss.config.js", "tailwind.config.js"):
        shutil.copy2(ROOT / name, site / name)
    for name in ("content", "layouts", "assets", "static"):
        shutil.copytree(ROOT / name, site / name)
    for name in ("node_modules", "_vendor"):
        (site / name).symlink_to(ROOT / name, target_is_directory=True)
    data = site / "data/r2/galleries"
    data.mkdir(parents=True)
    projects = list((site / "content").glob("*/_index.md"))
    (site / "data/legacy_photo_ids.json").write_text(json.dumps({
        project.parent.name.lower(): {"101": "999", "missing-photo": "888"} for project in projects
    }))
    for project in projects:
        slug = project.parent.name
        front = project.read_text()
        if re.search(r'^r2_gallery_id:', front, re.M):
            front = re.sub(r'^r2_gallery_id:.*$', f'r2_gallery_id: "{slug}"', front, count=1, flags=re.M)
        else:
            front = front.replace("\n---\n", f'\nr2_gallery_id: "{slug}"\n---\n', 1)
        project.write_text(front)
        entries = []
        for fid, tags, width, height in (("101", [], 640, 320), ("102", ["nogallery"], 320, 640), ("103", ["walnut"], 320, 640), ("104", ["gallery-cover"], 640, 320)):
            stem = f"{BASE}/photos/test/{fid}"
            entries.append({
                "id": fid, "title": f"Photo {fid}", "caption": "A <b>woodworking</b> photo &amp; details",
                "tags": tags, "source": f"{stem}/large.jpg", "width": width * 3, "height": height * 3,
                "renditions": {
                    "gallery": {"source": f"{stem}/gallery.jpg", "width": width, "height": height},
                    "thumbnail": {"source": f"{stem}/thumbnail.jpg", "width": 120, "height": 60},
                },
                "exif": {"model": "Test camera", "iso": "100"},
            })
        (data / f"{slug}.json").write_text(json.dumps({"schemaVersion": 1, "galleryId": slug, "entries": entries}))
    env = os.environ.copy()
    env["PATH"] = str(ROOT / "node_modules/.bin") + os.pathsep + env["PATH"]
    def build():
        return subprocess.run(["hugo", "--source", str(site), "--cacheDir", str(site / "cache"), "--minify"], env=env, capture_output=True, text=True)
    result = build()
    assert result.returncode == 0, result.stdout + result.stderr
    public = site / "public"
    home = (public / "index.html").read_text()
    redirects = (public / "_redirects").read_text().splitlines()
    assert len(redirects) == len(projects), redirects
    for project in projects:
        slug = project.parent.name.lower()
        assert f"/{slug}/999/ /{slug}/101/ 301" in redirects
        assert not any(f"/{slug}/888/" in redirect for redirect in redirects)
        assert f"/{slug}/" in home, f"Missing project card: {slug}"
        assert f"{BASE}/photos/test/104/gallery.jpg" in home
        gallery = (public / slug / "index.html").read_text()
        assert f'<meta property="og:image" content="{BASE}/photos/test/104/gallery.jpg"' in gallery
        assert "has-dimensions" in gallery
        assert "--photo-ratio:2" in gallery and "--photo-ratio:0.5" in gallery
        assert re.search(r'width=[\"\']?640[\"\']? height=[\"\']?320', gallery)
        assert f"/{slug}/101/" in gallery and f"/{slug}/103/" in gallery
        assert f"/{slug}/102/" not in gallery and f"/{slug}/104/" not in gallery
        first = (public / slug / "101/index.html").read_text()
        last = (public / slug / "103/index.html").read_text()
        assert re.search(r'rel=[\"\']?next[\"\']? href=[\"\']?/' + slug + r'/103/', first)
        assert re.search(r'rel=[\"\']?prev[\"\']? href=[\"\']?/' + slug + r'/101/', last)
        assert 'class="swiper photo-swiper"' in first
        assert re.search(r'data-current=[\"\']?true', first)
        assert re.search(r'data-photo-url=[\"\']?/' + slug + r'/103/', first)
        assert re.search(r'loading=[\"\']?lazy', first) and re.search(r'loading=[\"\']?eager', first)
        assert not re.search(r'rel=[\"\']?prefetch', first)
        assert 0 <= first.find("photo-swiper.min") < first.find("custom.min")
        assert "Test camera" in first and "ISO 100" in first
        assert "map[" not in first
        assert "A woodworking photo" in first
        for html in (gallery, first, last):
            assert "api.flickr.com" not in html and "staticflickr.com" not in html
        assert f"{BASE}/photos/test/101/gallery.jpg" in gallery
        assert f"{BASE}/photos/test/101/large.jpg" in first
        assert f"{BASE}/photos/test/103/large.jpg" in first
        assert f"{BASE}/photos/test/101/thumbnail.jpg" in first
        assert f'<meta property="og:image" content="{BASE}/photos/test/101/large.jpg"' in first
    if os.environ.get("GALLERY_PREVIEW_DIR"):
        shutil.copytree(public, os.environ["GALLERY_PREVIEW_DIR"], dirs_exist_ok=True)
    bad_file = next(data.glob("*.json"))
    manifest = json.loads(bad_file.read_text())
    manifest["entries"][-1]["tags"] = []
    bad_file.write_text(json.dumps(manifest))
    result = build()
    assert result.returncode != 0 and "has no gallery-cover photo" in result.stdout + result.stderr
    manifest["entries"][-1]["tags"] = ["gallery-cover"]
    manifest["galleryId"] = "wrong"
    bad_file.write_text(json.dumps(manifest))
    result = build()
    assert result.returncode != 0 and "Invalid R2 manifest" in result.stdout + result.stderr
    bad_file.unlink()
    result = build()
    assert result.returncode != 0 and "Missing R2 manifest" in result.stdout + result.stderr
    print(f"Verified {len(projects)} R2 project grids, photo pages, navigation, filtering, and manifest failures.")
