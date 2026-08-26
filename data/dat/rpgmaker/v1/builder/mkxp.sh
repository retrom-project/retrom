#!/bin/bash
set -euo pipefail

source /recipe/builder/common.sh

bootstrap_common_tools

copy_single_root "$LOCKED_INPUTS/mkxp-ruby.tar.xz" ruby-3.3.10 /work/host-ruby-source
(
	cd /work/host-ruby-source
	./configure --prefix=/work/host-ruby --disable-install-doc --disable-shared \
		--without-gmp --with-out-ext=dbm,gdbm,openssl,psych,readline,tk,zlib
	make -j1
	make install-nodoc
)

copy_single_root "$LOCKED_INPUTS/tool-ctags.tar.gz" ctags-6.1.0 /work/ctags
(
	cd /work/ctags
	./autogen.sh
	./configure --prefix="$TOOLS"
	make -j1
	make install
)

copy_single_root "$INPUTS/mkxp-z.tar.gz" \
	mkxp-z-f2efc98a344c505a66820e06d6508092719b8dd2 /work/mkxp
(
	cd /work/mkxp
	patch -p1 < /recipe/patches/mkxp-deterministic-build.patch
)

readonly STAGE1=/work/mkxp/libretro/build/libretro-stage1
readonly DOWNLOADS=/work/mkxp/libretro/build/downloads
mkdir -p "$STAGE1" "$DOWNLOADS/picosha2"
copy_single_root "$LOCKED_INPUTS/mkxp-ruby.tar.xz" ruby-3.3.10 "$DOWNLOADS/ruby"
copy_single_root "$LOCKED_INPUTS/mkxp-wabt.tar.gz" wabt-1.0.37 "$DOWNLOADS/wabt"
copy_single_root "$LOCKED_INPUTS/mkxp-libyaml.tar.gz" libyaml-0.2.5 "$DOWNLOADS/libyaml"
copy_single_root "$LOCKED_INPUTS/mkxp-zlib.tar.gz" zlib-1.3.1 "$DOWNLOADS/zlib"
copy_single_root "$LOCKED_INPUTS/mkxp-boost-predef.tar.gz" \
	predef-boost-1.90.0 "$STAGE1/boost_predef"
install -m 0644 "$LOCKED_INPUTS/mkxp-picosha2.h" "$DOWNLOADS/picosha2/picosha2.h"
rm -f "$DOWNLOADS/zlib/Makefile"

(
	cd "$DOWNLOADS/wabt"
	patch -p1 < /work/mkxp/libretro/wasm2c-data-segments.patch
)
(
	cd "$DOWNLOADS/ruby"
	for ruby_patch in ruby-compat.patch ruby-prng-time.patch ruby-lto.patch \
		ruby-sockets.patch ruby-user-threads.patch; do
		patch -p1 < "/work/mkxp/libretro/$ruby_patch"
	done
	printf '%s\n' '#include "/work/mkxp/libretro/ruby-bindings.h"' >> gc.c
)

copy_single_root "$LOCKED_INPUTS/mkxp-wasi-sdk.tar.gz" \
	wasi-sdk-30.0-x86_64-linux /work/wasi-sdk
copy_single_root "$LOCKED_INPUTS/mkxp-binaryen.tar.gz" \
	binaryen-version_126 /work/binaryen

# The release Ruby archive already contains configure. Equal normalized mtimes
# prevent the upstream network-only config.guess/config.sub refresh rule.
find "$DOWNLOADS" -exec touch -h -d @0 {} +
(
	cd /work/mkxp/libretro
	make -j1 \
		RUBY=/work/host-ruby/bin/ruby \
		CTAGS="$TOOLS/bin/ctags" \
		AUTORECONF="$TOOLS/bin/autoreconf" \
		ZIP=/recipe/builder/deterministic-zip.sh \
		WASI_SDK=/work/wasi-sdk \
		WASM_OPT=/work/binaryen/bin/wasm-opt
)

prepare_subproject() {
	local name=$1
	local archive=$2
	local root=$3
	local directory=${4:-$name}
	local source_dir="/work/mkxp/subprojects/$directory"
	copy_single_root "$LOCKED_INPUTS/$archive" "$root" "$source_dir"
	if [[ -d "/work/mkxp/subprojects/packagefiles/$name" ]]; then
		cp -a "/work/mkxp/subprojects/packagefiles/$name/." "$source_dir/"
	fi
	local declaration
	declaration=$(sed -n 's/^diff_files[[:space:]]*=[[:space:]]*//p' "/work/mkxp/subprojects/$name.wrap")
	if [[ -n "$declaration" ]]; then
		local patch_name
		while IFS= read -r patch_name; do
			(
				cd "$source_dir"
				patch -p1 < "/work/mkxp/subprojects/packagefiles/$patch_name"
			)
		done < <(printf '%s\n' "$declaration" | tr ',' '\n' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
	fi
}

prepare_subproject boost_asio mkxp-boost-asio.tar.gz asio-boost-1.90.0
prepare_subproject boost_assert mkxp-boost-assert.tar.gz assert-boost-1.90.0
prepare_subproject boost_config mkxp-boost-config.tar.gz config-boost-1.90.0
prepare_subproject boost_container_hash mkxp-boost-container-hash.tar.gz container_hash-boost-1.90.0
prepare_subproject boost_core mkxp-boost-core.tar.gz core-boost-1.90.0
prepare_subproject boost_describe mkxp-boost-describe.tar.gz describe-boost-1.90.0
prepare_subproject boost_mp11 mkxp-boost-mp11.tar.gz mp11-boost-1.90.0
prepare_subproject boost_optional mkxp-boost-optional.tar.gz optional-boost-1.90.0
prepare_subproject boost_preprocessor mkxp-boost-preprocessor.tar.gz preprocessor-boost-1.90.0
prepare_subproject boost_static_assert mkxp-boost-static-assert.tar.gz static_assert-boost-1.90.0
prepare_subproject boost_throw_exception mkxp-boost-throw-exception.tar.gz throw_exception-boost-1.90.0
prepare_subproject boost_type_traits mkxp-boost-type-traits.tar.gz type_traits-boost-1.90.0
prepare_subproject flac mkxp-flac.tar.gz flac-1.5.0
prepare_subproject fluidsynth mkxp-fluidsynth.tar.gz fluidsynth-2.5.0
# The FluidSynth release archive carries the empty git-submodule directory.
# Remove only that verified-empty placeholder so copy_single_root installs the
# locked GCEM tree at the exact path consumed by FindGCEM.cmake.
rmdir /work/mkxp/subprojects/fluidsynth/gcem
copy_single_root "$LOCKED_INPUTS/mkxp-gcem.zip" \
	gcem-012ae73c6d0a2cb09ffe86475f5c6fba3926e200 \
	/work/mkxp/subprojects/fluidsynth/gcem
test -f /work/mkxp/subprojects/fluidsynth/gcem/include/gcem.hpp
prepare_subproject freetype mkxp-freetype.tar.gz freetype-VER-2-14-1
prepare_subproject libiconv mkxp-libiconv.tar.gz libiconv-1.18 libiconv-1.18
prepare_subproject libidn mkxp-libidn.tar.gz libidn-1.43 libidn-1.43
prepare_subproject libretro-common mkxp-libretro-common.tar.gz \
	libretro-common-7caf0cd9448d5d745924c7e64e7365fb61ecda55
prepare_subproject libsndfile mkxp-libsndfile.tar.gz libsndfile-1.2.2
prepare_subproject mpg123 mkxp-mpg123.tar.gz mpg123-fe143d4e9c885ec34596c561481dff96357fd797
prepare_subproject ogg mkxp-ogg.tar.gz ogg-1.3.6
prepare_subproject openal-soft mkxp-openal-soft.tar.gz openal-soft-1.24.3
(
	cd /work/mkxp/subprojects/openal-soft
	patch -p1 < /recipe/patches/mkxp-openal-cmake-compat.patch
)
prepare_subproject egl-registry mkxp-egl-registry.tar.gz \
	EGL-Registry-3ae2b7c48690d2ce13cc6db3db02dfc0572be65e
prepare_subproject opengl-registry mkxp-opengl-registry.tar.gz \
	OpenGL-Registry-d38ff693f3e99ac5a61e3858de76c6c02976fa67
prepare_subproject opus mkxp-opus.tar.gz opus-1.5.2
prepare_subproject physfs mkxp-physfs.tar.gz physfs-release-3.2.0
prepare_subproject picosha2 mkxp-picosha2-source.tar.gz PicoSHA2-1.0.1
prepare_subproject pixman-region mkxp-pixman-region.tar.gz \
	pixman-region-6380f6e0fa6fdeed921cef39eb1d22d76f3f7014
prepare_subproject priority-deque mkxp-priority-deque.tar.gz \
	Priority-Deque-7475dacb65d112a4de7e5c24fe32cfffc2264fa9
prepare_subproject stb mkxp-stb.tar.gz stb-f1c79c02822848a9bed4315b12c8c8f3761e1296
prepare_subproject theora mkxp-theora.tar.gz theora-1.2.0
prepare_subproject theoraplay mkxp-theoraplay.tar.gz \
	theoraplay-672cf6d7591009612123e90a192e6f3cd9f532c2
prepare_subproject uchardet mkxp-uchardet.tar.xz uchardet-0.0.8 uchardet-0.0.8
prepare_subproject vorbis mkxp-vorbis.tar.gz vorbis-1.3.7
prepare_subproject zlib mkxp-zlib.tar.gz zlib-1.3.1

cat > /work/cross.ini <<'EOF'
[binaries]
c = 'emcc'
cpp = 'em++'
ar = 'emar'
cmake = 'cmake'
[host_machine]
system = 'emscripten'
cpu_family = 'wasm32'
cpu = 'wasm32'
endian = 'little'
[properties]
cmake_toolchain_file = '/emsdk/upstream/emscripten/cmake/Modules/Platform/Emscripten.cmake'
EOF

meson setup /work/mkxp-build /work/mkxp \
	--cross-file /work/cross.ini \
	--wrap-mode=nodownload \
	--buildtype release \
	-Db_lto=true \
	-Dlibretro=true \
	-Dlibretro_save_states=true \
	-Demscripten_threaded=true
ninja -C /work/mkxp-build -j1

copy_single_root "$INPUTS/retroarch.tar.gz" \
	RetroArch-69a4f0ea1e8aaf442ae4858f2e7f2b31a1776576 /work/retroarch
install -m 0644 /work/mkxp-build/mkxp-z_libretro.a /work/retroarch/libretro_emscripten.a

# Fail closed if the pinned RetroArch Makefile adds another Emscripten port or
# enables either conditional port/audio branch. The active port closure for
# this invocation is exactly zlib.
test "$(grep -Eo 'USE_[A-Z0-9_]+=[0-9]+' /work/retroarch/Makefile.emscripten | sort -u)" = \
	$'USE_SDL=2\nUSE_ZLIB=1'
grep -Fx 'HAVE_SDL2 = 0' /work/retroarch/Makefile.emscripten
grep -Fx 'HAVE_AL ?= 0' /work/retroarch/Makefile.emscripten

# Emscripten 4.0.8 consumes the same zlib 1.3.1 bytes already locked for
# mkxp. Seed its private writable ports/cache roots, verify both Retrom's
# SHA-256 and Emscripten's SHA-512, then build the sole port with networking
# still disabled for the whole container.
export EM_PORTS=/work/emscripten-ports
export EM_CACHE=/work/emscripten-cache
readonly EMSCRIPTEN_ZLIB_ARCHIVE="$EM_PORTS/zlib.tar.gz"
readonly EMSCRIPTEN_ZLIB_URL=https://github.com/madler/zlib/archive/refs/tags/v1.3.1.tar.gz
mkdir -p "$EM_PORTS/zlib" "$EM_CACHE"
install -m 0644 "$LOCKED_INPUTS/mkxp-zlib.tar.gz" "$EMSCRIPTEN_ZLIB_ARCHIVE"
test "$(wc -c < "$EMSCRIPTEN_ZLIB_ARCHIVE")" -eq 1572744
printf '%s  %s\n' \
	17e88863f3600672ab49182f217281b6fc4d3c762bde361935e436a95214d05c \
	"$EMSCRIPTEN_ZLIB_ARCHIVE" | sha256sum -c -
printf '%s  %s\n' \
	8c9642495bafd6fad4ab9fb67f09b268c69ff9af0f4f20cf15dfc18852ff1f312bd8ca41de761b3f8d8e90e77d79f2ccacd3d4c5b19e475ecf09d021fdfe9088 \
	"$EMSCRIPTEN_ZLIB_ARCHIVE" | sha512sum -c -
extract_archive "$EMSCRIPTEN_ZLIB_ARCHIVE" "$EM_PORTS/zlib"
printf '%s\n' "$EMSCRIPTEN_ZLIB_URL" > "$EM_PORTS/zlib/.emscripten_url"
embuilder build zlib
test -f "$EM_CACHE/sysroot/lib/wasm32-emscripten/libz.a"
test ! -L "$EM_CACHE/sysroot/lib/wasm32-emscripten/libz.a"
(
	cd /work/retroarch
	emmake make -j1 -f Makefile.emscripten \
		LIBRETRO=mkxp-z \
		HAVE_THREADS=1 \
		PROXY_TO_PTHREAD=1 \
		HAVE_AUDIOWORKLET=1 \
		HAVE_RWEBAUDIO=0 \
		HAVE_AL=0 \
		HAVE_SDL2=0 \
		HAVE_WASMFS=1 \
		HAVE_EXTRA_WASMFS=1
)

install -m 0644 /work/retroarch/mkxp-z_libretro.js /output/mkxp-z_libretro.js
install -m 0644 /work/retroarch/mkxp-z_libretro.wasm /output/mkxp-z_libretro.wasm
