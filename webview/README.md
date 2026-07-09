# webview/

Webview integrations for `workstation-probe`. The active integration is
the Hugo module in `hugo/`.

## Layout

| Subdirectory | Status | Purpose |
|---|---|---|
| `hugo/` | active | Hugo Module — embed panels via the `workstation-probe-panel` shortcode |

## Conventions

- Each integration owns its own `README.md`, package/module manifest, and
  source tree.
- Common product name (`workstation-probe`) and common API contract
  (`/metrics`, `/metrics/<module>`, `/metrics/<module>/history`) are
  shared across integrations.

For Hugo usage and module-development details, see `hugo/README.md`.
