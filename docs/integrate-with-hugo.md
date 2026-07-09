# Integrate workstation-probe webview into an existing Hugo blog

This guide walks through adding the live `workstation-probe` dashboard to an
existing Hugo blog. The webview is distributed as a Hugo Module — no source
copying, no manual file management. Hugo handles versioning and updates.

## Prerequisites

- **Hugo** ≥ 0.158.0 (`hugo version`)
- **Go** ≥ 1.21 (required by Hugo's module system; `go version`)
- An existing Hugo blog with a `go.mod` file (run `hugo mod init` if missing)
- A running `workstation-probe` server instance (see the project README)

## Step 1: Add the Hugo Module

Add the module to your site configuration. Pick the format matching your setup:

**`hugo.yaml`:**
```yaml
module:
  imports:
    - path: github.com/assaneko/workstation-probe/webview/hugo
```

**`hugo.toml`:**
```toml
[module]
  [[module.imports]]
    path = "github.com/assaneko/workstation-probe/webview/hugo"
```

**`config.toml` (legacy):**
```toml
[module]
  [[module.imports]]
    path = "github.com/assaneko/workstation-probe/webview/hugo"
```

Then download the module:

```bash
hugo mod get github.com/assaneko/workstation-probe/webview/hugo@latest
hugo mod tidy
```

Verify the module is listed in `go.mod`:

```bash
grep workstation-probe go.mod
```

Should show a line like:
```
require github.com/assaneko/workstation-probe/webview/hugo v0.1.0
```

## Step 2: Include the frontend scripts

In your base template (typically `layouts/_default/baseof.html`), add the
partial before the closing `</body>` tag:

```html
<head>
  ...
</head>
<body>
  ...
  {{ partial "workstation-probe/scripts.html" . }}
</body>
```

If you don't have a `baseof.html` yet, start from your theme's base template:

```bash
cp themes/your-theme/layouts/_default/baseof.html layouts/_default/baseof.html
```

Then add the `{{ partial "workstation-probe/scripts.html" . }}` line before
`</body>`.

## Step 3: Create a panel config

Each panel needs a YAML config file. Create `configs/rig-01.yaml` (or any
name you prefer):

```yaml
name: rig-01
api: "http://localhost:19090"
refresh: "5s"
modules:
  - cpu
  - memory
  - gpu
  - storage
```

- `name` — displayed in the panel header
- `api` — the `workstation-probe` server base URL (no trailing slash)
- `refresh` — poll interval as a Go duration string (`5s`, `10s`, `30s`, `1m`)
- `modules` — which metric panels to show (one or more of: `cpu`, `memory`, `gpu`, `storage`)

> **Important:** Always quote the `refresh` value (`"5s"`, not `5s`) and the
> `api` URL. Hugo's YAML parser is strict about unquoted scalars containing
> colons or special characters.

For a self-contained blog post, you can place the config as a page resource
inside a [leaf bundle](https://gohugo.io/content-management/page-bundles/):

```
content/posts/my-monitor-post/
├── index.md
└── rig-01.yaml
```

Then reference it with `{{< workstation-probe-panel "./rig-01.yaml" >}}` in
the markdown page.

## Step 4: Add the shortcode to a page

In any markdown content file, insert the shortcode:

```markdown
---
title: "My Workstation Dashboard"
date: 2026-07-08
---

## Live Metrics

{{< workstation-probe-panel "rig-01.yaml" >}}
```

The shortcode argument is resolved in this order:
1. **Page resource** — if the current page is a leaf bundle and contains a file matching the argument
2. **Project configs/** — falls back to `configs/<arg>` in the site root

## Step 5: Test locally

Run the Hugo development server:

```bash
hugo server
```

Visit `http://localhost:1313` and navigate to the page with the shortcode.
You should see a live metrics panel rendering data from the server.

## CORS configuration

If your Hugo dev server and the `workstation-probe` server run on different
ports, enable CORS in the server's `config.yaml`:

```yaml
security:
  cors:
    enabled: true
    allowed_origins:
      - http://localhost:1313
```

For production, if Hugo and the `workstation-probe` server are served behind
the same reverse proxy (nginx, Caddy), CORS is unnecessary.

## Troubleshooting

### "module not found" or "unknown revision"

Run `hugo mod get` and `hugo mod tidy` again. Make sure your site has a
`go.mod` file. If the site was created before Hugo Modules were introduced,
run:

```bash
hugo mod init github.com/your-username/your-blog
```

### "shortcode not found" or "workstation-probe-panel not found"

The module import is not being picked up. Check:
1. The `hugo.yaml`/`hugo.toml` has the correct `module.imports` section
2. `go.mod` lists the module in its `require` block
3. You're running `hugo server` from the site root, not a subdirectory

### "YAML unmarshal error" or "wrong number of fields"

The panel config YAML has unquoted values. Make sure `api` and `refresh`
values are quoted:

```yaml
# WRONG
api: http://localhost:19090
refresh: 5s

# CORRECT
api: "http://localhost:19090"
refresh: "5s"
```

### "CORS error" or "Failed to fetch" in browser console

The browser is blocking cross-origin requests. Enable CORS on the server
(see the CORS section above), or serve both the site and the server from
the same origin.

### "Chart.js is not defined" or blank panel

The Chart.js CDN isn't loading. If you're behind a firewall or on an
intranet without internet access, self-host Chart.js. Download
`chart.js@4.4.6` (the `+esm` build) into
`static/workstation-probe/js/vendor/chart.js` and change the import URL in
`static/workstation-probe/js/charts.js` to `./vendor/chart.js`.

### Tests fail with "Hugo version mismatch"

The module requires Hugo ≥ 0.158.0. Check your Hugo version:

```bash
hugo version
```

If you have an older version, upgrade Hugo before importing the module.
