# Platform illustrations

Original SVG source illustrations created for Retrom. Hardware uses simplified
console, handheld, arcade or computer silhouettes; software runtimes use themed
illustrations (for example, RPG Maker's map and pencil, KiriKiri's book and quill).
These are illustrative symbols, not official logos or exact hardware diagrams.
Related platforms may share an illustration. No third-party artwork is included.

`web/features/home/platform-art.ts` explicitly maps every platform declared in
`data/runtime-target-bindings/v1/catalog.json` to these assets. When adding a
platform, add a suitable drawing or intentionally reuse a family illustration.
The SVG files here are the editable source; no raster masters are required.

Keep each SVG at most **4 KiB** and the entire SVG collection at most **128 KiB**.
Use paths and simple shapes, without embedded images, remote references, fonts
or scripts. The platform-art tests enforce catalog coverage, valid SVG and size
limits. The homepage serves the assets locally and loads rail images lazily.
Unknown IDs and loading failures keep an empty slot with the same dimensions.
