#!/usr/bin/env bash
set -euo pipefail

# Keep verified visual-test fonts and their license notices in this checkout.
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
font_root="$repository_root/.cache/tools/e2e-fonts"
mkdir -p "$font_root/cache"

prepare_font() (
  package_url="$1" package_sha="$2" package_directory="$font_root/$3"
  font_file="$package_directory/$4" font_sha="$5"
  if printf '%s  %s\n' "$font_sha" "$font_file" | sha256sum --check --status 2>/dev/null; then
    exit 0
  fi
  archive="$(mktemp "$font_root/package.XXXXXX.deb")"
  trap 'rm -f -- "$archive"' EXIT
  curl --fail --location --silent --show-error --retry 3 "$package_url" --output "$archive"
  printf '%s  %s\n' "$package_sha" "$archive" | sha256sum --check --status
  dpkg-deb --extract "$archive" "$package_directory"
  printf '%s  %s\n' "$font_sha" "$font_file" | sha256sum --check --status
)

prepare_font \
  'https://archive.ubuntu.com/ubuntu/pool/main/f/fonts-android/fonts-droid-fallback_8.1.0r7-1~1.gbp36536b_all.deb' \
  'ac7a01e81d97546d5af8c9a61a475f8bffb383c8a1eeb80e994ce38d411ed1f5' \
  'package' 'usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf' \
  'acb6440a713d880a13a21b468ba7cd43f5a2b2934972e51be791c880730777b8'
prepare_font \
  'https://archive.ubuntu.com/ubuntu/pool/main/f/fonts-dejavu/fonts-dejavu-core_2.37-8_all.deb' \
  '40049660c194f3b8a2541fc7369efebb10e9f94bdac836a2f38fafedd10fa73a' \
  'dejavu' 'usr/share/fonts/truetype/dejavu/DejaVuSans.ttf' \
  'ae7b7855e115a5966d8b1b3f80f254ccc117ec86f9965e202ee2940453837280'

python3 - "$font_root" <<'PY'
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

root = Path(sys.argv[1])
config = ET.Element("fontconfig")
ET.SubElement(config, "include").text = "/etc/fonts/fonts.conf"
for directory in ("package/usr/share/fonts/truetype/droid", "dejavu/usr/share/fonts/truetype/dejavu"):
    ET.SubElement(config, "dir").text = str(root / directory)
ET.SubElement(config, "cachedir").text = str(root / "cache")
# Match Linux system-ui/default sans metrics across developer and CI hosts.
for family in ("system-ui", "sans-serif", "Arial"):
    alias = ET.SubElement(config, "alias", binding="strong")
    ET.SubElement(alias, "family").text = family
    prefer = ET.SubElement(alias, "prefer")
    ET.SubElement(prefer, "family").text = "DejaVu Sans"
    ET.SubElement(prefer, "family").text = "Droid Sans Fallback"
ET.ElementTree(config).write(root / "fonts.conf", encoding="utf-8", xml_declaration=True)
PY
printf 'fontconfig=%s/fonts.conf\n' "$font_root"
