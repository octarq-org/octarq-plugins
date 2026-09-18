# Octarq plugin template

A starter for building an [Octarq](https://github.com/octarq-org/octarq) plugin —
a full-stack feature composed into a host **at build time, without forking**.
An Octarq plugin is one repo with two mirror halves:

- **Backend** — a Go module implementing `plugin.Plugin` (`plugin.go`).
- **Frontend** — a JS package implementing `UIPlugin` from
  [`@octarq/plugin-sdk`](https://www.npmjs.com/package/@octarq/plugin-sdk) (`web/`).

> Octarq's own core features (links, mail, DNS) are built this exact way — the
> Pro edition is just another set of these plugins. Anything they do, your
> plugin can too.

## Use it

1. Click **“Use this template”** (or `gh repo create you/octarq-plugin-foo --template octarq-org/octarq-plugin-template`).
2. Make it yours — rename in four places, all currently `myplugin`:
   - `go.mod` module path → your repo path.
   - Go package name, `Plugin.Name()` and the menu `ID`/`Path` in `plugin.go`.
   - `name` in `web/index.ts` (**must equal** `Plugin.Name()`) and its route path.
   - `name` in `web/package.json`.
3. Resolve deps:
   ```bash
   go mod tidy
   cd web && pnpm install
   ```

## Layout

```
.
├── go.mod            # Go module: github.com/you/octarq-plugin-foo
├── plugin.go         # backend: implements plugin.Plugin (+ MenuProvider)
└── web/
    ├── index.ts      # frontend: implements UIPlugin (@octarq/plugin-sdk)
    ├── Page.tsx      # your lazy-loaded page
    ├── package.json
    └── tsconfig.json
```

The backend exposes one guarded endpoint (`GET /api/myplugin/ping`); the page
fetches it and renders with the shared SDK UI, handling the standard
402 (unlicensed) / 404 (not in this build) gated states.

## Build it into a host

Plugins are composed at build time (like `xcaddy` — pick plugins, build a
binary), not loaded at runtime.

**Backend** — a host `main.go` imports your module and mounts it:

```go
app, _ := app.New()
app.Use(myplugin.Plugin{})
app.Run(ctx)
```

**Frontend** — the host lists your package in its plugin manifest
(`web/octarq.plugins.json`), or injects it without editing files:

```bash
OCTARQ_PLUGINS='["octarq-plugin-myplugin"]' make docker-build
```

A build whose manifest doesn't name your package ships none of your UI bytes.

## Develop

```bash
go build ./...     # type-check the backend against the Octarq core
go vet ./...
cd web && pnpm exec tsc --noEmit   # type-check the frontend (needs the SDK installed)
```

## Going further

This template is intentionally minimal. The backend `plugin.Context` also gives
you a database, secret encryption, audit log, notifications, transactional +
inbound email hooks, DNS control, cache, geo/UA parsing, webhooks, a background
job queue, inter-plugin services, and **MCP tools** (implement `MCPProvider` and
your feature becomes drivable by AI agents). See the full author guide:

**https://github.com/octarq-org/octarq/blob/main/docs/PLUGINS.md**

## License

[MIT](LICENSE).
