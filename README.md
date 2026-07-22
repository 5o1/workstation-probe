# workstation-probe demo site

Live demo of [workstation-probe](https://github.com/5o1/workstation-probe) built with
Hugo + the Book theme, using a 60-second loop of synthetic real-time metrics.

The demo is deployed to GitHub Pages from the `demo` branch. It does not require a
running monitor server — the `sample-stream` data generator runs entirely in the
browser.

## Local development

```bash
hugo serve
# Open http://localhost:1313/workstation-probe/docs/
```

## How it works

- The panel config sets `api: "sample-stream"` instead of a real server URL.
- `sample-stream.js` generates a deterministic 60-second loop of metrics for a
  simulated workstation (32 cores, 512 GB RAM, 1 × 24 GB GPU, 1 TB disk).
- `poller.js` detects the `sample-stream` API and routes through the local
  generator, bypassing the network entirely.
