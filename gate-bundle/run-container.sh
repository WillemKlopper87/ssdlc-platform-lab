#!/bin/sh
# Operator-owned wrapper invoked by trusted-gate-runner.py. It deliberately
# requires an immutable image digest and gives the scanner container no
# network, credentials, Docker socket, or writable application checkout.
set -eu

case "${GATE_BUNDLE_IMAGE:?GATE_BUNDLE_IMAGE is required}" in
  *@sha256:*) ;;
  *) echo "gate-container: GATE_BUNDLE_IMAGE must use an immutable @sha256 digest" >&2; exit 2 ;;
esac

: "${GATE_WORKSPACE:?GATE_WORKSPACE is required}"
: "${GATE_OUTPUT_DIR:?GATE_OUTPUT_DIR is required}"

docker run --rm \
  --network none \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --mount "type=bind,src=${GATE_WORKSPACE},dst=/workspace,readonly" \
  --mount "type=bind,src=${GATE_OUTPUT_DIR},dst=/output" \
  -e GATE_WORKSPACE=/workspace \
  -e GATE_OUTPUT_DIR=/output \
  "${GATE_BUNDLE_IMAGE}"
