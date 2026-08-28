"""Small, dependency-free signed gate-attestation format.

The key is held only by the future trusted runner and the bot sidecar. HMAC is
used for this pilot transition because it is easy to operate locally; the
durable design in SADR-0017 replaces it with a separately managed signing key
and verified release provenance.
"""
import hashlib
import hmac
import json
from pathlib import Path


def canonical_bytes(document):
    """Return stable JSON bytes, excluding the detached signature field."""
    unsigned = {key: value for key, value in document.items() if key != "signature"}
    return json.dumps(unsigned, sort_keys=True, separators=(",", ":")).encode("utf-8")


def contract_digest(contract):
    return "sha256:" + hashlib.sha256(
        json.dumps(contract, sort_keys=True, separators=(",", ":")).encode("utf-8")
    ).hexdigest()


def policy_digest(directory):
    """Deterministic digest over every file's (relative path, content hash) under
    directory, sorted by path.

    Detects any change to the bundle's scanning policy -- a rule added, removed,
    or edited anywhere under policy/, including policy/vendored-rules/'s 594
    files -- without needing to enumerate or reason about which specific file
    changed. Content-based (not mtime/size-based) so a byte-identical rebuild
    always reproduces the same digest.
    """
    directory = Path(directory)
    entries = [
        f"{path.relative_to(directory).as_posix()}:{hashlib.sha256(path.read_bytes()).hexdigest()}"
        for path in sorted(directory.rglob("*"))
        if path.is_file()
    ]
    return "sha256:" + hashlib.sha256("\n".join(entries).encode("utf-8")).hexdigest()


def sign(document, key):
    signed = dict(document)
    signed["signature"] = "hmac-sha256:" + hmac.new(
        key.encode("utf-8"), canonical_bytes(document), hashlib.sha256
    ).hexdigest()
    return signed


def signature_is_valid(document, key):
    signature = document.get("signature", "")
    if not isinstance(signature, str) or not signature.startswith("hmac-sha256:"):
        return False
    expected = sign(document, key)["signature"]
    return hmac.compare_digest(signature, expected)
