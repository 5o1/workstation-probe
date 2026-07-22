---
title: "Workstation Probe — Live Demo"
type: docs
---

Real-time system metrics from a simulated 32-core / 512 GB / 1 GPU workstation.

## How the test data is generated

This public demo is a browser-only test page. Its panel configuration uses
`api: "sample-stream"`, so the browser does not contact a backend service.
Every second, `sample-stream.js` produces the same data shape as the real
`/metrics` and `/profile` endpoints and passes it to the normal dashboard
renderer.

The values describe a simulated 32-core CPU, 512 GB of memory, one 24 GB GPU,
and a 1 TB disk. CPU, memory, GPU, and temperature values vary smoothly from
time-based signals. CPU busy-core selection uses a fixed seed, and the whole
scenario repeats every 60 seconds. Storage capacity is fixed at 42% used. The
deterministic loop makes the page safe to host as static files and gives the
renderer realistic changing input without exposing a real machine.

{{< workstation-probe-panel "./sample-stream.yaml" >}}

## Deploying with real data

GitHub Pages can only host the static dashboard; it cannot run the
`workstation-probe` backend service. For a real deployment, build and run the
`workstation-probe` binary on each machine being observed, configure its
listening address and port, and serve it through HTTPS. For example:

```bash
make build
sudo make install
sudo systemctl enable --now workstation-probe
```

Then change the panel config's `api` value from `"sample-stream"` to the
`workstation-probe` service's base URL:

```yaml
name: "Production Workstation"
api: "https://workstation.example.com"
refresh: "5s"
modules:
  - cpu
  - memory
  - gpu
  - storage
```

The browser must be able to reach that URL. If the dashboard and
`workstation-probe` use different origins, enable CORS for the dashboard
origin; putting both behind the same reverse proxy avoids CORS. Protect the
service with the access controls required by the environment rather than
exposing an unauthenticated metrics endpoint to the public internet. Keep
`sample-stream` for demos and use a separate, environment-specific panel
config for production.
