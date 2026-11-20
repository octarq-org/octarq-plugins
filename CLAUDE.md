# Working in this repo (octarq-plugins)

Conventions for official plugins (`dns`, `links`, `mail`, `maillink`, `pt`, `twofa`) and `_template` for [Octarq](https://github.com/octarq-org/octarq).

## Toolchain

- **Go 1.27+**, standard-library `http.ServeMux`. Each plugin is an independent Go module (`<plugin>/go.mod`).
- **Frontend (`*/web/`)**: React + TS + Tailwind + `@octarq/plugin-sdk`. Package manager is **pnpm 11** (`pnpm-workspace.yaml` at repo root).
- **Never use npm.** Always use `pnpm`.
- `go.work` is gitignored: CI and downstream users build each plugin module with `GOWORK=off` against its pinned `octarq/server` version.

## Verify before saying "done"

Run these inside your worktree and confirm they pass:
- Frontend (from repo root):
  - `pnpm check-types`
  - `pnpm lint:ui`
  - `pnpm test`
- Go (for any touched or affected plugin directory):
  - `cd <plugin> && GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test ./... -race`
  - `gofmt -w .`

## Change Workflow

- **All changes go through an isolated git worktree** (under `/Volumes/PHD/code/.worktrees/`, one branch per change). Never develop directly on `main`.
- **Git identity**: Always commit as `Jungley Yeh <yejing@live.cn>`. Never add `Co-authored-by` trailers.
- **Pre-v1.0 Stage**:
  - No fallback, soft-degradation, or dual-track compatibility code.
  - Remove deprecated interfaces/fields directly rather than keeping shims.
  - Fail closed on missing configuration or invalid inputs.
- **No comments in code**: `.go`, `.ts`, `.tsx`, and every other source file carry no comments. Only `SPEC.md`, `docs/`, `README`, and `PRODUCT.md` hold design notes; commit messages hold the why. Keep compiler and tool directives (`//go:build`, `//go:embed`, `//nolint`, `eslint-disable`, `@ts-expect-error`, `/* ui-color-ok */`).

## Design System Contract

All UI in `*/web/` must satisfy the 3-Surface Design System (aligned with `octarq/CLAUDE.md`, verified by `pnpm lint:ui` and Vitest):

1. **Three-Surface Luminance Law**: Every surface belongs strictly to Frame (`--sidebar`), Panel (`--background`), or Card/Row (`--card` / `--well`), defined in `oklch` neutral steps in the host `styles.css`. Never invent a 4th background surface, use hardcoded `bg-white` (unless annotated with `/* ui-color-ok */` for QR quiet zones / HTML email paper), or substitute drop shadows for `1px` borders.
2. **Closed SDK Layout Primitives**: Pages compose `@octarq/plugin-sdk` primitives (`PageShell`, `PageSection`, `PageRows`, `PageRow`, `PageCard`, `PageEmpty`, `Separator`, `StatusBadge`, `Field`, `DataTable`/`Table`). `PageShell` always renders at full content width. Prefer `PageSection` -> `PageRows` + `Separator` over stacked individual cards ("card soup").
3. **Single Control Knobs & Spatial Scale**: Use `--radius: 0.5rem` (`rounded-lg`) derivations and `--row-height: 3.5rem` (`h-14` / `min-h-[var(--row-height,3.5rem)]`). Avoid oversized `rounded-xl`/`rounded-2xl` containers.
4. **Geometric Loading Skeletons (Zero Layout Shift)**: Never render bare text loaders (`"Loading..."`, `"Loading analytics..."`, etc.) for initial data loads. Use `<TableSkeleton>` for tables and geometric `<Skeleton>` / `PageRows` skeletons matching real row height (`min-h-[var(--row-height,3.5rem)]`) or card/form geometry so content never jumps when loaded.
5. **Semantic Status Vocabulary**: Use `StatusBadge` with semantic tones (`idle`, `running`, `info`, `success`, `done`, `warning`, `paused`, `error`, `blocked`, `review`) or semantic `Badge` tones (`default`, `info`, `success`, `warning`, `danger`, `secondary`, `outline`) instead of raw color names (`green`, `amber`, `red`, `cyan`, `indigo`, `violet`, `neutral`).
6. **Anti-Patterns (CI Enforced via `pnpm lint:ui`)**:
   - No decorative background gradients or ambient glows (`.octarq-aurora`, `blur-3xl`).
   - No `backdrop-blur` outside modal/drawer/palette backdrop overlays.
   - No heavy shadows (`shadow-xl`, `shadow-2xl`) or bouncy translate/scale hover/entrance effects (`hover:-translate-y-px`, `hover:scale-*`, `.animate-rise`).
   - No native `alert()`, `confirm()`, or `prompt()`; use `toast` and `confirmDialog` from `@octarq/plugin-sdk`.
