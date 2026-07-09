# webview/hugo — Hugo Module

> 📖 **New to workstation-probe?** Follow the [integration guide](../../docs/integrate-with-hugo.md)
> to add the webview to an existing Hugo blog.


A Hugo Module that embeds live `workstation-probe` dashboards inside
markdown blog posts via the `workstation-probe-panel` shortcode.

The shortcode name uses the full project name (not an abbreviation) so
it does not collide with the generic `panel` shortcode that many Hugo
themes and modules already ship. The same naming rule applies to all
CSS classes, data attributes, file paths, and partial names — see the
"Namespace" section below.

This directory is the **module** — what you import into your existing
Hugo site. It ships:

- `layouts/shortcodes/workstation-probe-panel.html` — the shortcode
- `layouts/partials/workstation-probe/scripts.html` — include in baseof.html
- `static/workstation-probe/css/panel.css` — styles, scoped under the project name
- `static/workstation-probe/js/*.js` — boot / poller / render / charts
- `hugo.yaml` — module metadata (name, license, min Hugo version)
- `go.mod` — Go module declaration

It does **not** ship a `content/` or `configs/` directory; those are the
caller's responsibility.

For local development of this module itself, see
[`exampleSite/basic/`](exampleSite/basic/). The same site is
also the snapshot-test target — see `test/README.md`.

## Namespace

Every name this module exposes uses the full project name `workstation-probe-`
as the prefix. No abbreviations, no shared terms. This is a defensive
choice — the module is meant to drop into arbitrary Hugo sites that may
already ship their own `panel` shortcode, `.panel` CSS class, etc.

| Surface | Name | Why no collision |
|---|---|---|
| Shortcode | `workstation-probe-panel` | other modules' `panel` shortcode is untouched |
| Partial | `workstation-probe/scripts.html` | unique path |
| Static files | `static/workstation-probe/...` | served at `/workstation-probe/...` |
| CSS classes | `.workstation-probe-panel`, `.workstation-probe-sub`, ... | scoped, not `panel` |
| CSS variables | `--workstation-probe-bg`, etc. | declared inside the root class, cascade only within |
| Data attributes | `data-workstation-probe-config`, `data-workstation-probe-value`, ... | unique namespace |

## Install

In your existing Hugo site:

```bash
# If you haven't initialized Go modules for the site yet:
cd my-blog
hugo mod init github.com/me/my-blog
# Or, if your site is already a Go module, just add the import:

hugo mod get github.com/assaneko/workstation-probe/webview/hugo@latest
```

In your site's `hugo.yaml`:

```yaml
module:
  imports:
    - path: github.com/assaneko/workstation-probe/webview/hugo
```

In your site's `layouts/_default/baseof.html` (or wherever you load
site-wide scripts), include the panel frontend:

```html
{{ partial "workstation-probe/scripts.html" . }}
```

For a starting `baseof.html`, copy from `exampleSite/basic/layouts/_default/baseof.html`.

## Usage

In any markdown page:

```markdown
{{< workstation-probe-panel "./rig-01.yaml" >}}
```

The argument is a config filename. The shortcode resolves it in this
order:

1. **Page resource** — `Page.Resources.Get <arg>`. Lets a blog post
   colocate its panel config in a [leaf bundle][bundle], so the post is
   fully self-contained.
2. **Project-relative `configs/<arg>`** — fallback for shared configs
   that many pages reference.

[bundle]: https://gohugo.io/content-management/page-bundles/

Examples:

```text
content/posts/incident-2026-07-04/      ← leaf bundle
├── index.md                            ← {{< workstation-probe-panel "./rig-01.yaml" >}}
└── rig-01.yaml                         ← page resource, picked up first

content/posts/other.md                  ← plain file
# {{< workstation-probe-panel "rig-01.yaml" >}}    ← no bundle resource, falls back to configs/rig-01.yaml
```

## Config schema

```yaml
# in any of: page resource, configs/<name>.yaml
name: rig-01                  # shown in the panel header
api: "http://localhost:19090" # workstation-probe server base URL (no trailing slash)
refresh: "5s"                 # poll interval — Go duration: 500ms, 5s, 2m
modules:                      # which sub-panels to render
  - cpu
  - memory
  - gpu
  - storage
```

`refresh` accepts the same grammar the server's `sampler.interval` uses
(milliseconds/seconds/minutes). The browser parses it client-side; see
`static/workstation-probe/js/poller.js::parseDurationMs`.

Quoting matters: Hugo's `transform.Unmarshal` uses a strict YAML parser
that rejects ambiguous scalars. Write `"5s"` (quoted), not `5s` (unquoted),
or the build will fail with `record on line N: wrong number of fields`.
Same for URLs with colons.

## CORS

`workstation-probe`'s default config does not enable CORS. When this site
runs on a different origin than the `workstation-probe` server (e.g. dev
server on `http://localhost:1313` and the API on `http://localhost:19090`),
configure the server's CORS section:

```yaml
security:
  cors:
    enabled: true
    allowed_origins:
      - http://localhost:1313
```

For a single-origin deploy (Hugo served behind the same reverse proxy as
the `workstation-probe` server), this is unnecessary.

## Architecture

```
markdown page
  └─ {{< workstation-probe-panel "./rig-01.yaml" >}} (Hugo shortcode)
       └─ <div class="workstation-probe-panel"        (HTML emitted by shortcode)
              data-workstation-probe-config='{...}'>
            └─ boot.js (entry, loaded once via baseof.html)
                 ├─ parses config, renders static shell
                 └─ subscribes to poller.js
                      └─ one fetch per (api, refresh) pair
                           └─ render.js
                                └─ charts.js (Chart.js)
```

The shared poller (`static/workstation-probe/js/poller.js`) deduplicates
requests: N panels on a page pointing at the same `api` share a single
fetch cycle and receive the data through a Set of callbacks. Different
`api` URLs or different `refresh` values get independent pollers.

## Layout

- `hugo.yaml` — module metadata (name, description, license, min Hugo version)
- `go.mod` — Go module declaration
- `layouts/shortcodes/workstation-probe-panel.html` — the one shortcode
- `layouts/partials/workstation-probe/scripts.html` — partial users include
- `static/workstation-probe/css/panel.css` — styles, scoped under the project name
- `static/workstation-probe/js/boot.js` — entry, runs once at page load
- `static/workstation-probe/js/poller.js` — shared polling per (api, refresh)
- `static/workstation-probe/js/charts.js` — Chart.js wrapper (CDN import)
- `static/workstation-probe/js/render.js` — per-module renderers

## Vendoring Chart.js

The CDN import in `static/workstation-probe/js/charts.js` requires
internet at page load. To self-host, download `chart.js@4.4.6` (the
`+esm` build) into
`static/workstation-probe/js/vendor/chart.js` and change the import URL
in `charts.js` to a relative `./vendor/chart.js`.
