#!/usr/bin/env bash
set -euo pipefail

# Headless Linux runners need a real CJK fallback, not .notdef rectangles.
# Keep the package, Apache-2.0 notice and Fontconfig configuration in this checkout.
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
font_root="$repository_root/.cache/tools/e2e-fonts"
font_file="$font_root/package/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf"
package_sha="ac7a01e81d97546d5af8c9a61a475f8bffb383c8a1eeb80e994ce38d411ed1f5"
font_sha="acb6440a713d880a13a21b468ba7cd43f5a2b2934972e51be791c880730777b8"
package_url="https://archive.ubuntu.com/ubuntu/pool/main/f/fonts-android/fonts-droid-fallback_8.1.0r7-1~1.gbp36536b_all.deb"

mkdir -p "$font_root/cache"
if ! printf '%s  %s\n' "$font_sha" "$font_file" | sha256sum --check --status 2>/dev/null; then
  archive="$(mktemp "$font_root/package.XXXXXX.deb")"
  trap 'rm -f -- "$archive"' EXIT
  curl --fail --location --silent --show-error --retry 3 "$package_url" --output "$archive"
  printf '%s  %s\n' "$package_sha" "$archive" | sha256sum --check --status
  dpkg-deb --extract "$archive" "$font_root/package"
  printf '%s  %s\n' "$font_sha" "$font_file" | sha256sum --check --status
fi

python3 - "$font_root" <<'PY'
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

root = Path(sys.argv[1])
config = ET.Element("fontconfig")
ET.SubElement(config, "include").text = "/etc/fonts/fonts.conf"
ET.SubElement(config, "dir").text = str(root / "package/usr/share/fonts/truetype/droid")
ET.SubElement(config, "cachedir").text = str(root / "cache")
ET.ElementTree(config).write(root / "fonts.conf", encoding="utf-8", xml_declaration=True)
PY
printf 'fontconfig=%s/fonts.conf\n' "$font_root"
