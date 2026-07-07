# Intranet NAS mirror

This project supports a zero-budget deployment model where the public blog
stays on GitHub Pages, while the live workstation dashboard is served by an
intranet NAS mirror.

## Goal

The public site should only link to the intranet page. It must not iframe the
dashboard and must not fetch workstation APIs from GitHub Pages:

```text
public GitHub Pages blog
  -> normal link to http://nas-host-or-ip/lab-blog/posts/workstation-monitor/

intranet NAS mirror
  -> same Hugo theme, same navigation, same article layout
  -> fetches http://workstation-lan-ip:19090 from the browser
```

This avoids browser mixed-content / private-network restrictions because the
HTTPS public page does not fetch private HTTP resources. It also keeps live
metrics inside the LAN.

## Repository roles

Use one canonical GitHub repository and one private NAS overlay:

```text
GitHub repository
  public content, theme, workstation-probe Hugo module, no real LAN config

NAS overlay
  private intranet page, workstation IPs/hostnames, NAS baseURL override

NAS build workspace
  clean repo clone + overlay copied on top, then hugo build
```

Do not edit the NAS clone directly. Build from a clean clone plus overlay every
time so `git pull` cannot be blocked by local changes.

## Overlay example

`webview/hugo/nas-overlay.example/` is a committed template for the private
NAS overlay. Copy it to the NAS and edit the placeholders there. The real
overlay should normally live outside the Git repository, for example:

```text
/volume1/lab-blog/overlay/
  config/intranet/hugo.yaml
  content/posts/workstation-monitor/index.md
  content/posts/workstation-monitor/ws01.yaml
  content/posts/workstation-monitor/ws02.yaml
```

The example uses page-local YAML files beside the monitor article, so each
panel config is resolved through the shortcode's page-relative lookup.

## Build composition

A NAS builder can compose the site with ordinary file copies:

```bash
rsync -a --delete repo/ work/
rsync -a overlay/ work/
hugo --source work --environment intranet --destination publish --cleanDestinationDir
rsync -a --delete publish/ /volume1/web/lab-blog/
```

`--environment intranet` makes Hugo read `config/intranet/hugo.yaml` from the
overlay. Use that file to override `baseURL` for the NAS mirror.

## Public link page

The GitHub Pages version should use a normal link:

```markdown
[Open the intranet workstation monitor](http://nas-host-or-ip/lab-blog/posts/workstation-monitor/)
```

Do not use an iframe, and do not embed `workstation-probe-panel` on the public
GitHub Pages page unless the APIs are also served over public HTTPS.

## Workstation CORS

When the NAS page fetches a workstation API on a different host/port, each
workstation must allow the NAS origin. The origin is only scheme + host + port;
it does not include the path:

```yaml
server:
  host: 0.0.0.0       # or a specific LAN address
  port: 19090

security:
  cors:
    enabled: true
    allowed_origins:
      - http://nas-host-or-ip
      - http://nas-host-or-ip:1313
  rate_limit:
    enabled: true
    requests_per_second: 10
    burst: 20
    trust_proxy_headers: false
    exempt_paths: [/health]
```

Bind only to the LAN interface when possible, and use host firewall rules if
the workstation has any interface outside the trusted lab network.

## Asset note

The Hugo module imports Chart.js from a CDN by default. If the NAS mirror must
work without internet access, vendor Chart.js into the consuming site and
override `static/workstation-probe/js/charts.js` to import the local vendored
copy. See `webview/hugo/README.md` for the vendoring note.

## Security boundary

- Live metrics stay on the LAN; the public site contains only a link.
- Internet visitors can see the link but cannot reach RFC1918/LAN addresses.
- CORS is not authentication. It only controls browser reads from allowed
  origins.
- Do not commit the real NAS overlay when it contains hostnames, IPs, tokens,
  or lab-specific topology.
