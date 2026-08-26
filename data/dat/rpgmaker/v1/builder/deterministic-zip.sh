#!/bin/bash
set -euo pipefail

if [[ $# -lt 3 || $1 != -r ]]; then
	printf '%s\n' 'RPG_RUNTIME_BUILD_ZIP_ARGUMENTS_INVALID' >&2
	exit 64
fi

readonly ARCHIVE=$2
shift 2
if [[ -e "$ARCHIVE" || -L "$ARCHIVE" ]]; then
	printf '%s\n' 'RPG_RUNTIME_BUILD_ZIP_OUTPUT_EXISTS' >&2
	exit 64
fi

readonly LIST_ROOT=$(mktemp -d "$TMPDIR/retrom-rpg-zip.XXXXXX")
readonly UNSORTED_LIST="$LIST_ROOT/unsorted"
readonly SORTED_LIST="$LIST_ROOT/sorted"
cleanup_lists() {
	rm -f -- "$UNSORTED_LIST" "$SORTED_LIST"
	rmdir -- "$LIST_ROOT"
}
trap cleanup_lists EXIT

# Info-ZIP recursively enumerates directories in filesystem order. Two clean
# source trees can therefore contain identical files but produce different
# archive bytes. Enumerate every entry ourselves, reject the newline that
# Info-ZIP's -@ protocol cannot represent, then feed one C-locale order.
for root in "$@"; do
	if [[ ! -e "$root" && ! -L "$root" ]]; then
		printf '%s\n' "RPG_RUNTIME_BUILD_ZIP_INPUT_MISSING:$root" >&2
		exit 64
	fi
	while IFS= read -r -d '' path; do
		if [[ $path == *$'\n'* ]]; then
			printf '%s\n' 'RPG_RUNTIME_BUILD_ZIP_FILENAME_INVALID' >&2
			exit 64
		fi
		printf '%s\0' "$path" >> "$UNSORTED_LIST"
	done < <(find "$root" -print0)
done

LC_ALL=C sort -z -u "$UNSORTED_LIST" > "$SORTED_LIST"
while IFS= read -r -d '' path; do
	touch -h -d @0 "$path"
done < "$SORTED_LIST"

# GNU Make exports ZIP as the path to this wrapper. Info-ZIP also interprets
# ZIP/ZIPOPT as implicit command-line arguments, so they must not reach zip.
unset ZIP ZIPOPT
tr '\0' '\n' < "$SORTED_LIST" | /usr/bin/zip -X -@ "$ARCHIVE"
