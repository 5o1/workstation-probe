# webview/

Webview frontends for `workstation-probe`. Each subdirectory is a
self-contained integration targeting one site-framework. The convention
is one subdirectory per framework so each can be versioned and published
independently.

## Layout

| Subdirectory | Status | Purpose |
|---|---|---|
| `hugo/` | active | Hugo Module — embed panels via the `workstation-probe-panel` shortcode |
| `hugo/test/` | active | Snapshot tests for `hugo/` AND the local dev site. One site serves both purposes — see below. |
| `astro/` | planned | Astro component (future) |
| `nextjs/` | planned | Next.js component (future) |

## Conventions

- Each `<framework>/` is independent: own `README.md`, own package/module
  manifest, own source tree.
- Common product name (`workstation-probe`) and common API contract
  (`/metrics`, `/metrics/<module>`, `/metrics/<module>/history`) are
  shared across frameworks — only the embed mechanism differs.

## Hugo Module — development & testing

`hugo/exampleSite/basic/` is the canonical site for both:

- **Dev preview** — `hugo server -D` for live iteration against the
  module. Any change in `hugo/layouts/` or `hugo/static/` is picked up
  immediately because `hugo/exampleSite/basic/go.mod` points the module
  import at `../..` (the hugo module's own directory).
- **Snapshot test** — `webview/hugo/test/run.sh` builds the same site
  and diffs against `webview/hugo/test/expected/basic/`. Catches
  byte-level regressions in module output.

Because both flows use the same site, the goldens always reflect the
content the dev server renders.

To develop:

```bash
cd webview/hugo/exampleSite/basic
hugo server -D
# Open http://localhost:1313 — pages render through the module's shortcode.
```

To run the snapshot test:

```bash
cd webview/hugo/test
./run.sh           # verify against goldens
./run.sh --update  # overwrite goldens after an intentional change
```

The site resolves the `workstation-probe` Hugo module via a relative
path in its `go.mod` (`replace ... => ../..`). No absolute path is
stored anywhere in the repo, so cloning to a different home directory
needs no edits.
