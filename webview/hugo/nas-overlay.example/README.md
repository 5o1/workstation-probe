# NAS overlay example

This directory is a template for a private intranet overlay. Copy it to the
NAS, edit the placeholder hostnames/IPs, and apply it on top of a clean clone
before running `hugo --environment intranet`.

Example NAS-side location:

```text
/volume1/lab-blog/overlay/
```

Files:

- `config/intranet/hugo.yaml` overrides `baseURL` for the NAS mirror.
- `content/posts/workstation-monitor/index.md` is the intranet article.
- `content/posts/workstation-monitor/ws01.yaml` and `ws02.yaml` are sample
  panel configs.

The real overlay should not be committed if it contains lab-specific hostnames,
IP addresses, tokens, or any other private topology.

See `docs/intranet-mirror.md` from the repository root for the full deployment
model.
