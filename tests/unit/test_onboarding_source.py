#!/usr/bin/env python3
"""Guard against regressing private-repository onboarding authentication."""
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[2] / "scripts" / "onboard-repo.sh"
SOURCE = SCRIPT.read_text(encoding="utf-8")

assert 'git -c http.extraHeader="Authorization: token ${GITEA_ADMIN_TOKEN}" clone' in SOURCE
assert 'git -c http.extraHeader="Authorization: token ${GITEA_ADMIN_TOKEN}" push' in SOURCE
assert '${GITEA_ADMIN_TOKEN}@' not in SOURCE

print("PASS private clone and push both use an HTTP authorization header")
