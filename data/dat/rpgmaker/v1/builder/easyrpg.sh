#!/bin/bash
set -euo pipefail

source /recipe/builder/common.sh

bootstrap_common_tools

readonly BUILDSCRIPTS=/work/buildscripts
readonly PREFIX=/work/easyrpg-prefix
copy_single_root "$INPUTS/easyrpg-buildscripts.tar.gz" \
	buildscripts-57e4736179f65e910a399267f4ce4ea68d382667 "$BUILDSCRIPTS"
(
	cd "$BUILDSCRIPTS"
	patch -p1 < /recipe/patches/easyrpg-fixed-parallel.patch
)

declare -ar archives=(
	'easy-zlib.tar.gz:zlib-1.3.1'
	'easy-libpng.tar.xz:libpng-1.6.39'
	'easy-freetype.tar.xz:freetype-2.13.3'
	'easy-harfbuzz.tar.xz:harfbuzz-10.4.0'
	'easy-pixman.tar.gz:pixman-0.44.2'
	'easy-expat.tar.bz2:expat-2.7.0'
	'easy-ogg.tar.xz:libogg-1.3.5'
	'easy-vorbis.tar.xz:libvorbis-1.3.7'
	'easy-mpg123.tar.bz2:mpg123-1.32.10'
	'easy-libsndfile.tar.xz:libsndfile-1.2.2'
	'easy-libxmp-lite.tar.gz:libxmp-lite-4.6.2'
	'easy-speexdsp.tar.gz:speexdsp-1.2.1'
	'easy-opus.tar.gz:opus-1.5.2'
	'easy-opusfile.tar.gz:opusfile-0.12'
	'easy-fluidsynth.tar.gz:fluidsynth-2.4.3'
	'easy-nlohmann-json.tar.gz:json-3.11.3'
	'easy-inih.tar.gz:inih-r58'
	'easy-fmt.zip:fmt-11.1.4'
	'easy-icu.tar.gz:icu'
	'easy-sdl2.tar.gz:SDL2-2.32.2'
)

for declaration in "${archives[@]}"; do
	archive=${declaration%%:*}
	root=${declaration#*:}
	copy_single_root "$LOCKED_INPUTS/$archive" "$root" "$PREFIX/$root"
done

extract_archive "$LOCKED_INPUTS/easy-icudata.tar.gz" "$PREFIX"
copy_single_root "$INPUTS/liblcf.tar.gz" \
	liblcf-92c4450a1bc1acb58bd02bbb99b57e5036919cdf "$PREFIX/liblcf"

(
	cd "$PREFIX"
	NO_CCACHE=1 USE_WASM_SIMD=0 BUILD_LIBLCF=1 \
		"$BUILDSCRIPTS/emscripten/2_build_toolchain.sh"
)

copy_single_root "$INPUTS/easyrpg-player.tar.gz" \
	Player-78328fa29f465315291e161130e6682f69410370 /work/player
(
	cd /work/player
	patch -p1 < /recipe/patches/easyrpg-retrom-bridge.patch
)

emcmake cmake -S /work/player -B /work/player-build -G Ninja \
	-DCMAKE_BUILD_TYPE=Release \
	-DCMAKE_PREFIX_PATH="$PREFIX" \
	-DCMAKE_FIND_ROOT_PATH="$PREFIX" \
	-DSDL2_DIR="$PREFIX/lib/cmake/SDL2" \
	-DPLAYER_FIND_ROOT_PATH_APPEND=ON \
	-DPLAYER_ENABLE_TESTS=OFF \
	-DPLAYER_JS_BUILD_SHELL=OFF
cmake --build /work/player-build --parallel 1

install -m 0644 /work/player-build/easyrpg-player.js /output/easyrpg-player.js
install -m 0644 /work/player-build/easyrpg-player.wasm /output/easyrpg-player.wasm
