<!-- BEGIN:nextjs-agent-rules -->

# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->


# UI consistency is a required delivery gate

- Before changing UI, read `../docs/ui-specification.md` and its unified design source. Use `styles/tokens.css` for typography, weights, radii, color roles and spacing; `styles/controls.css` owns shared buttons, form fields and selectors. Feature CSS arranges controls and must not redefine their appearance or shadow these tokens.
- Do not add raw font sizes/weights, inline typography, ad hoc Tailwind typography, unlayered global feature CSS, or `!important` to defeat shared controls. Extend a semantic token or shared variant only when its role is justified and documented; never introduce a per-page exception or historical allowlist.
- Run `make web-ui-check` and `make acceptance-case CASE=ACC-UI-011` from the repository root for UI changes, plus the affected functional cases. Both are CI gates. On first use, prepare the pinned browser with `make prepare-e2e-browser` and initialize acceptance evidence with `make acceptance-prepare`; resolve recorded failures through the defect ledger before rerunning. Keep screenshots of the actual changed pages and states, including 390px mobile and physical 3840×2160 at 150% scaling (2560×1440 CSS viewport, DPR 1.5); compare them to the unified design and the previous version.
- Inspect visual hierarchy, cover proportions, whitespace, long titles, empty/populated states, focus and overlays. An automated PASS does not replace this visual review. Do not accept an unintended regression by changing expectations, skipping a viewport, weakening a rule or replacing evidence with a mock screen.
- Intentional visual changes must update the existing design fragment and UI contract, then regenerate the review HTML. Do not hand-edit the generated review or commit local screenshot artifacts. Report what was visually reviewed and any unmet acceptance criteria.

- Typography or control styling does not authorize changing card geometry, page sections or navigation behavior. Preserve existing aspect ratios, desktop card widths and the 4/6/8/12px radius scale unless the user requests those changes. Verify composite inputs in both idle and focused states: only their outer shell paints the border/focus ring, and inner fields/icons stay contained and vertically centered.
