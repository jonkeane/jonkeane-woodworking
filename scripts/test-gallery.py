#!/usr/bin/env python3
"""Build isolated Flickr fixtures; never replace a developer's imported metadata."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import struct
import zlib

ROOT = Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory(prefix="woodworking-gallery-") as tmp:
    site = Path(tmp)
    for name in ("hugo.toml", "go.mod", "go.sum", "package.json", "postcss.config.js", "tailwind.config.js"):
        shutil.copy2(ROOT / name, site / name)
    for name in ("content", "layouts", "assets", "static"):
        shutil.copytree(ROOT / name, site / name, ignore=shutil.ignore_patterns("flickr") if name == "assets" else None)
    for name in ("node_modules", "_vendor"):
        (site / name).symlink_to(ROOT / name, target_is_directory=True)
    data = site / "data/flickr/photosets"
    data.mkdir(parents=True)
    images = site / "assets/flickr"
    images.mkdir(parents=True)
    def png(width, height):
        def chunk(kind, data):
            return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
        return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0))
                + chunk(b"IDAT", zlib.compress((b"\x00" + b"\x80\x60\x40" * width) * height)) + chunk(b"IEND", b""))
    for fid in ("101", "102", "103"):
        for variant in ("thumbnail", "full"):
            (images / f"{fid}-{variant}.png").write_bytes(png(640, 320) if fid == "101" else png(320, 640))
    projects = list((site / "content").glob("*/_index.md"))
    import re
    for project in projects:
        album = re.search(r'flickr_album: "(\d+)"', project.read_text()).group(1)
        photos = []
        for fid, tags in (("101", []), ("102", ["nogallery"]), ("103", ["walnut"])):
            photos.append({
                "id": fid, "title": f"Photo {fid}", "tags": tags,
                "description": {"_content": "A <b>woodworking</b> photo &amp; details"},
                "url_z": "https://live.staticflickr.com/fixture_z.jpg",
                "url_k": "https://live.staticflickr.com/fixture_k.jpg",
                "thumbnail_asset": f"flickr/{fid}-thumbnail.png", "full_asset": f"flickr/{fid}-full.png",
                "url_sq": "https://live.staticflickr.com/fixture_s.jpg", "exif": {"model": "Test camera", "iso": "100"},
                "width_z": 640 if fid == "101" else "320",
                "height_z": "320" if fid == "101" else 640,
            })
        (data / f"{project.parent.name}.json").write_text(json.dumps({
            "stat": "ok", "photoset": {"id": album, "photo": photos}
        }))
    env = os.environ.copy()
    env["PATH"] = str(ROOT / "node_modules/.bin") + os.pathsep + env["PATH"]
    subprocess.run(["hugo", "--source", str(site), "--cacheDir", str(site / "cache"), "--minify"], env=env, check=True)
    public = site / "public"
    home = (public / "index.html").read_text()
    for project in projects:
        slug = project.parent.name.lower()
        assert f"/{slug}/" in home, f"Missing project card: {slug}"
        gallery = (public / slug / "index.html").read_text()
        assert "has-dimensions" in gallery
        assert "--photo-ratio:2" in gallery and "--photo-ratio:0.5" in gallery
        assert re.search(r'width=[\"\']?640[\"\']? height=[\"\']?320', gallery)
        assert f"/{slug}/101/" in gallery and f"/{slug}/103/" in gallery
        assert f"/{slug}/102/" not in gallery
        assert not (public / slug / "102/index.html").exists()
        first = (public / slug / "101/index.html").read_text()
        last = (public / slug / "103/index.html").read_text()
        assert re.search(r'rel=[\"\']?next[\"\']? href=[\"\']?/' + slug + r'/103/', first)
        assert re.search(r'rel=[\"\']?prev[\"\']? href=[\"\']?/' + slug + r'/101/', last)
        assert 'class="swiper photo-swiper"' in first
        assert re.search(r'data-current=[\"\']?true', first)
        assert re.search(r'data-photo-url=[\"\']?/' + slug + r'/103/', first)
        assert re.search(r'loading=[\"\']?lazy', first) and re.search(r'loading=[\"\']?eager', first)
        assert not re.search(r'rel=[\"\']?prefetch', first)
        assert 0 <= first.find("photo-swiper.min") < first.find("custom.min"), "Site overrides must follow Swiper CSS"
        assert "Test camera" in first and "ISO 100" in first
        assert "map[" not in first, "Description must be text, not a serialized map"
        assert "A woodworking photo" in first, first[first.index("<figcaption"):first.index("</figcaption>")]
        for html in (gallery, first, last):
            assert "nanogallery" not in html and "api.flickr.com" not in html
            assert "staticflickr.com" not in html
        assert "/flickr/101-thumbnail.png" in gallery
        assert "/flickr/101-full.png" in first and "/flickr/103-full.png" in first
        assert "https://mokkou.jonkeane.com/flickr/101-full.png" in first
        for fid in ("101", "103"):
            for variant in ("thumbnail", "full"):
                assert (public / f"flickr/{fid}-{variant}.png").read_bytes() == (images / f"{fid}-{variant}.png").read_bytes()
    if os.environ.get("GALLERY_PREVIEW_DIR"):
        shutil.copytree(public, os.environ["GALLERY_PREVIEW_DIR"], dirs_exist_ok=True)
    # Legacy caches must remain usable without a network fetch at build time.
    legacy_file = next(data.glob("*.json"))
    legacy = json.loads(legacy_file.read_text())
    for photo in legacy["photoset"]["photo"]:
        del photo["width_z"], photo["height_z"]
    legacy_file.write_text(json.dumps(legacy))
    subprocess.run(["hugo", "--source", str(site), "--cacheDir", str(site / "cache"), "--minify"], env=env, check=True)
    legacy_gallery = (public / legacy_file.stem.lower() / "index.html").read_text()
    assert "has-dimensions" in legacy_gallery
    # Missing downloads must fail instead of silently using remote images.
    image_file = images / "101-full.png"
    image_bytes = image_file.read_bytes()
    image_file.unlink()
    missing = subprocess.run(["hugo", "--source", str(site)], env=env, capture_output=True, text=True)
    assert missing.returncode != 0 and "Missing local Flickr" in missing.stdout + missing.stderr
    image_file.write_bytes(image_bytes)
    # No cached metadata must produce a clear build failure, not an empty gallery.
    next(data.glob("*.json")).unlink()
    missing = subprocess.run(["hugo", "--source", str(site), "--cacheDir", str(site / "cache")], env=env, capture_output=True, text=True)
    assert missing.returncode != 0 and "Missing Flickr data" in missing.stdout + missing.stderr
    print(f"Verified {len(projects)} project grids, photo pages, navigation, filtering, and missing-data failure.")
