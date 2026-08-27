# Retrom public RPG Maker smoke fixtures

This directory contains redistributable projects for the RPG Maker 2000, 2003,
XP, VX, VX Ace, and MV product acceptance paths, plus Retrom-owned MV/MZ
malicious-shape projects and the deterministic `ACC-RPG-010` input matrix. It
contains no RTP, commercial editor output, BIOS, key, proprietary default
script, or executable third-party payload. `fixture-spec/` and `generator/`
are the unique sources; every engine, malicious, and negative-matrix file is a
generated output locked by `fixture-manifest.json`.

`rpg2000/` is the normal generation fixture. `rpg2000-compat/` is a second
fully playable RPG2000 project produced by the same bounded LCF writer with a
different title, visible marker, palette, and therefore content identity. It
exists only so `ACC-RPG-012` can create an old binding and a distinct new
binding through two real Retrom imports; uploading `rpg2000/` twice is not an
acceptable substitute.

Run `python3 build.py` to regenerate the outputs, or `python3 build.py --check`
to rebuild in an ignored temporary directory and compare every path and byte.
The LCF writer implements only the bounded structures required by these two
fixtures. Its output was parsed in full with liblcf commit
`92c4450a1bc1acb58bd02bbb99b57e5036919cdf`; the attribution license is in
`LICENSES/liblcf-MIT.txt`.

MV is the only fixture with a third-party runtime input. `vendor/manifest.json`
locks the MIT `rpgtkoolmv/corescript` v1.3b commit, source archive, license,
component provenance, and archive SHA-256. The generator rejects drift in the
archive, license, six ordered core concatenations, and six library files before
writing output. All MV `data/`, plugin, HTML, CSS, image, and WAV content is
Retrom-owned; no default database, font, icon, asset, or plugin is copied from
an editor installation.

The LCF projects start directly on a 20x15 passable map, play a project-owned
250 ms tone, and show a generation-specific marker. Moving right from the
`(10,8)` start onto `(11,8)` changes variable 1 from 0 to 1; moving through
`(12,8)` onto `(13,8)` changes it from 1 to 2. Both state events use the
below-player collision trigger, so neither event blocks movement and the state
transition does not depend on a confirm-key mapping or the initial facing.
The RGSS projects contain one project-owned
Ruby program in a deterministic Ruby Marshal 4.8 array with a deterministic
stored-zlib member. Arrow input moves a visible block and the confirm input
cycles variable 1 through 0, 1, and 2. Rendering uses only solid rectangles so
the fixture does not depend on an RTP or host font. Their globals match Retrom's
read-only position bridge, so checkpoint acceptance can compare map, x/y, and variable.
The MV project enters a standard `Scene_Map`, moves the standard `$gamePlayer`,
cycles `$gameVariables[1]` from 0 to 1 to 2 on confirm, and saves only through
the standard `DataManager` pipeline. Its marker PNG and short WAV are generated
from Retrom source.

`malicious-rpgmv/` and `malicious-rpgmz/` are small Retrom-owned browser shapes,
not vendor engines and not evidence that arbitrary MZ games run. They implement
only the standard globals needed by the native bridge, deterministic movement,
save/load, canvas rendering, and isolation probes. The MZ source deliberately
carries four unreferenced Retrom text payloads named `.exe`, `.dll`, `.node`,
and `.bat`: import must preserve their exact source bytes and files digest,
while the runtime projection must never publish them. No native payload is
executed.

`negative-matrix/matrix.json` is the machine-readable source plan for 42
internal cross-generation combinations, all of which are exact mismatches;
the user-facing virtual core performs the only automatic generation choice.
The plan also contains 13 ambiguity/safety inputs and 70 nested-archive
overlays. The latter cross seven generations with ZIP/7z/RAR/TAR/gzip detected
both by extension and by magic. These are project files: the acceptance driver
uploads their locked bytes through Retrom and proves they are never recursively
opened. The outer traversal, symlink, and high-ratio ZIPs are separate inputs
that must be rejected before a review or Launch exists.

These structural properties do not by themselves prove runtime support. The
corresponding `ACC-RPG-002` through `ACC-RPG-007`, `010`, and `011` cases must import these files
through Retrom, enter the actual runtime, and restore point B in a different
Launch. API success, static marker inspection, same-Launch load, and successful
Marshal/LCF parsing are not substitutes for that product evidence.
