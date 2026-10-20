# Roost

The order of work is the checklist in issue #1. Issue #2 has the stack and layout.

## Commands

- `make dev` runs the Go server on 127.0.0.1:4700 and Vite, which proxies `/api` to it. The Go side doesn't reload, so restart after Go changes.
- `make build` builds `./roost` with the UI embedded, using the `ui` build tag. Plain `go build`, `go test` and `go vet` leave the UI out, so they work without a web build.
- `make test` runs `go test -race ./...` and `svelte-check`.
- `make types` regenerates `web/src/lib/api/types.ts` from `internal/api/types.go` with tygo. Never edit the generated file by hand.
- The web app uses Bun as its package manager (`cd web && bun add ...`).

## Front end: Svelte 5 and SvelteKit 3

`web/` uses **Svelte 5** (runes) and **SvelteKit 3**. SvelteKit 3 came out on 2026-10-01 and has many breaking changes, and most examples online (and most of what you remember) are SvelteKit 2. Don't write SvelteKit APIs from memory: look them up with the Svelte MCP server (`list-sections`, then `get-documentation`), and run `svelte-autofixer` on components you write. Never use Svelte 4 syntax (`export let`, `$:`, `on:click`, `<slot>`, stores for component state).

The app is built with `adapter-static` as a single-page app and embedded in the Go binary, which serves `/api`. SvelteKit's server features (`+page.server.ts`, `hooks.server.ts`, form actions, remote functions, cookies, CSRF) aren't used, so their v3 changes are left out below. If that changes, read the full guide: `get-documentation "kit/migrating-to-sveltekit-3"`.

### SvelteKit 3: write this, not that

| | Not this (v2) | This (v3) |
|---|---|---|
| Config | `svelte.config.js` with a `kit: {…}` block | Pass options to `sveltekit({…})` in `vite.config.ts`. Former `kit.*` options are top-level, next to `compilerOptions`. `svelte.config.js` is no longer supported. |
| `src/lib` alias | `$lib/foo` | `#lib/foo.js` (also for `foo.ts`) and `#lib/Thing.svelte`: always with an extension. Declared in `package.json` `imports`: `"#lib": "./src/lib/index.js"`, `"#lib/*": "./src/lib/*"`. |
| Page state | `$app/stores` (`$page`, `$navigating`, `$updated`) | `$app/state` (`page`, `navigating`, `updated`), no `$` prefix. `$app/stores` is removed. |
| `page.url` | Mutate it in place | It's readonly. Copy it first: `new URL(page.url.href)`. |
| Environment | `$app/environment`, `$env/static/public` | `$app/env`, `$app/env/public` (`$env/*` is deprecated) |
| Paths | `base`, `assets`, `resolveRoute` from `$app/paths` | `resolve('/blog/[slug]', { slug })`, `resolve('blog/hello')`, `asset('logo.png')`. Only route IDs start with `/`. |
| Reloading data | `invalidateAll()` | `refreshAll()`. Clicking a link to the current page now calls it too. |
| `goto` options | `invalidateAll`, `noScroll`/`keepFocus`, `replaceState` | `refreshAll`, `reset: false`, `replace` |
| Shallow routing | `pushState(url, state)`, `replaceState(url, state)` | `goto(url, { shallow: true, state })`, adding `replace: true` to replace |
| External URLs | `goto('https://…')` | `window.location.href = url`. `goto` rejects URLs outside the app. |
| Link attributes | `data-sveltekit-preload-data="off"`, `data-sveltekit-noscroll`, `data-sveltekit-keepfocus` | `="false"`, `data-sveltekit-reset="false"` |
| `preloadData` | Assume `loaded` or `redirect` | Also handle `{ type: 'error', status, error }`. |
| Navigation events | `delta` on every navigation | `delta` only on `popstate` (back/forward) |
| `error()` | `error(404, { message, code })` | `error(404, 'Not found', { code })`. `App.Error` always has `status`. |
| `handleError` | Only called for unexpected errors | Called for every error, including `error(...)` and rendering errors. It can return `status`. |
| Types | `Page` and `Handle` from `@sveltejs/kit` | `Page` from `$app/state`, hook types from `@sveltejs/kit/hooks`, `defineParams` from `@sveltejs/kit/params` |
| Param matchers | One file per matcher in `src/params/` | One `src/params.ts` using `defineParams` (functions or Standard Schema) |
| tsconfig | `extends: "./.svelte-kit/tsconfig.json"` | `extends: "$app/tsconfig"` with explicit `include` and `exclude` |
| Service worker | `$service-worker` | `version` from `$app/env`, `assets`/`immutable`/`prerendered` from `$app/manifest`, plus `$app/paths`. Needs its own `src/service-worker/tsconfig.json`. |

Watch out for server-only modules. Any file with a `server` segment in its name (`server.ts`, `x.server.ts`) and any `server/` directory outside `src/routes` is server-only, and a client-only SPA can't import it. Don't name front-end modules or folders `server`; use something like `#lib/hub/`.

Minimum versions: Node 22.17, TypeScript 6, Svelte 5.57.1, Vite 8.0.12, `@sveltejs/vite-plugin-svelte` 7.
