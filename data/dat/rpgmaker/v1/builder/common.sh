#!/bin/bash
set -euo pipefail

export HOME=/work/home
export LANG=C
export LC_ALL=C
export SOURCE_DATE_EPOCH=0
export TZ=UTC
export TMPDIR=/work/tmp
export ZERO_AR_DATE=1
umask 022

readonly INPUTS=/inputs
readonly LOCKED_INPUTS=/inputs/locked-sources
readonly TOOLS=/work/tools

mkdir -p "$HOME" "$TMPDIR" "$TOOLS/bin" /work/sources
export PATH="$TOOLS/bin:$PATH"

extract_archive() {
	local archive=$1
	local destination=$2
	mkdir -p "$destination"
	case "$archive" in
		*.zip) unzip -q "$archive" -d "$destination" ;;
		*.tar.gz) tar --no-same-owner -xzf "$archive" -C "$destination" ;;
		*.tar.xz) tar --no-same-owner -xJf "$archive" -C "$destination" ;;
		*.tar.bz2) tar --no-same-owner -xjf "$archive" -C "$destination" ;;
		*) echo "RPG_RUNTIME_BUILD_ARCHIVE_UNSUPPORTED:$archive" >&2; return 1 ;;
	esac
}

build_autotool() {
	local archive=$1
	local root=$2
	local source_dir="/work/sources/$root"
	extract_archive "$LOCKED_INPUTS/$archive" /work/sources
	(
		cd "$source_dir"
		./configure --prefix="$TOOLS"
		make -j1
		make install
	)
}

bootstrap_common_tools() {
	build_autotool tool-m4.tar.xz m4-1.4.19
	build_autotool tool-autoconf.tar.xz autoconf-2.71
	build_autotool tool-automake.tar.xz automake-1.16.5
	build_autotool tool-libtool.tar.xz libtool-2.4.7
	build_autotool tool-pkgconf.tar.xz pkgconf-1.9.5
	ln -s "$TOOLS/bin/pkgconf" "$TOOLS/bin/pkg-config"

	extract_archive "$LOCKED_INPUTS/tool-ninja.tar.gz" /work/sources
	(
		cd /work/sources/ninja-1.12.1
		python3 configure.py --bootstrap
		install -m 0755 ninja "$TOOLS/bin/ninja"
	)

	extract_archive "$LOCKED_INPUTS/tool-meson.tar.gz" /work/sources
	ln -s /work/sources/meson-1.7.0/meson.py "$TOOLS/bin/meson"
	meson --version | grep -Fx '1.7.0'
}

copy_single_root() {
	local archive=$1
	local root=$2
	local destination=$3
	local staging
	staging=$(mktemp -d /work/extract.XXXXXX)
	extract_archive "$archive" "$staging"
	test -d "$staging/$root"
	mkdir -p "$(dirname "$destination")"
	mv "$staging/$root" "$destination"
	rmdir "$staging"
}
