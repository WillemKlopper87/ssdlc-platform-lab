# Pilot gate bundle

Build this directory only from the SSDLC Platform repository root. The image
contains pinned scanner binaries, the same vendored Semgrep rules used by the
fast gate, normalisers, and the Rego severity policy. It is the command
authority for the trusted runner; an application PR cannot change it.

```sh
docker build -f gate-bundle/Dockerfile -t ssdlc-gate:pilot-2026-08-26 .
```

The runner should invoke a fixed, absolute operator-owned wrapper such as
`gate-bundle/run-container.sh`. Set `GATE_BUNDLE_IMAGE` to the pushed image's
immutable `@sha256:` digest; the wrapper rejects tags. It starts the image with
the untrusted checkout mounted read-only and a writable output directory. Do
not mount the Docker socket, the attestation directory, forge tokens, or the
bot token into this image.

Trivy's vulnerability database is downloaded during the image build and copied
into the final image. Runtime scans use `--skip-db-update` with no network.
Rebuild and test a new bundle release on the platform's patch cadence to update
that database; do not give a merge-decision container Internet access merely to
refresh it.

Add or update rules only through a reviewed bundle release, then update
`gate-contract/contract.json`, record its new digest, and test it against a
pilot repository before enforcing it.
