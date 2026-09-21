#!/usr/bin/env python3
"""UAT bootstrap steps that need the Gitea/Woodpecker HTTP APIs.

Runs inside the ssdlc-uat-ops container (see setup-uat.ps1). Idempotent:
every step tolerates "already exists". Configuration comes from the
environment (state/uat.env); results are printed as KEY=VALUE lines that
setup-uat.ps1 merges back into that file.

  gitea        org, teams, members, pilot + exceptions repos, OAuth apps
  wp-token     mint a durable Woodpecker API token for the admin
  wp-secrets   register the pipeline secrets on the pilot repo
  repo-id      print the Woodpecker repo id of the pilot repo
"""
import os
import re
import sys

import requests

ORG = "ssdlc"
PILOT = "pilot-app"
EXCEPTIONS = "exceptions"
GITEA = os.environ["GITEA_URL"].rstrip("/")
WP = os.environ["WOODPECKER_URL"].rstrip("/")
SERVER_IP = os.environ["SERVER_IP"]
ADMIN = os.environ.get("ADMIN_USER", "gateadmin")
ADMIN_TOKEN = os.environ.get("GITEA_ADMIN_TOKEN", "")

APPROVERS = ["gateadmin", "dev1"]
DEVELOPERS = ["dev1", "dev2", "dev3", "gate-reporter"]


def api(method, path, **kw):
    return requests.request(
        method, f"{GITEA}/api/v1{path}",
        headers={"Authorization": f"token {ADMIN_TOKEN}"}, timeout=30, **kw)


def ok(r, *allowed):
    if r.status_code >= 300 and r.status_code not in allowed:
        sys.exit(f"ERROR {r.request.method} {r.request.url}: {r.status_code} {r.text[:300]}")
    return r


def ensure_team(name, description):
    found = api("GET", f"/orgs/{ORG}/teams/search", params={"q": name}).json().get("data") or []
    for t in found:
        if t["name"] == name:
            return t["id"]
    # Gitea versions differ on units_map vs permission; try both shapes.
    payloads = [
        {"name": name, "description": description, "includes_all_repositories": True,
         "can_create_org_repo": False,
         "units_map": {"repo.code": "write", "repo.issues": "write", "repo.pulls": "write"}},
        {"name": name, "description": description, "includes_all_repositories": True,
         "can_create_org_repo": False, "permission": "write",
         "units": ["repo.code", "repo.issues", "repo.pulls"]},
    ]
    r = None
    for p in payloads:
        r = api("POST", f"/orgs/{ORG}/teams", json=p)
        if r.status_code < 300:
            return r.json()["id"]
    sys.exit(f"ERROR creating team {name}: {r.status_code} {r.text[:300]}")


def ensure_repo(name, auto_init):
    if api("GET", f"/repos/{ORG}/{name}").status_code == 200:
        return
    ok(api("POST", f"/orgs/{ORG}/repos", json={
        "name": name, "private": True, "auto_init": auto_init,
        "default_branch": "main"}))


def ensure_oauth(name, redirect):
    """Return (client_id, client_secret); the secret is only shown on creation.
    An existing app with the same name is replaced: its redirect URL may
    hold a previous server address."""
    r = api("GET", "/user/applications/oauth2")
    if r.status_code == 200:
        for app in r.json():
            if app.get("name") == name:
                api("DELETE", f"/user/applications/oauth2/{app['id']}")
    r = ok(api("POST", "/user/applications/oauth2", json={
        "name": name, "redirect_uris": [redirect], "confidential_client": True,
        "skip_secondary_authorization": True}))
    d = r.json()
    return d["client_id"], d["client_secret"]


def phase_gitea():
    if api("GET", f"/orgs/{ORG}").status_code != 200:
        ok(api("POST", "/orgs", json={"username": ORG, "visibility": "private"}))
    approvers = ensure_team("approvers", "Second-party exception approvers")
    developers = ensure_team("developers", "UAT developers")
    for user in APPROVERS:
        ok(api("PUT", f"/teams/{approvers}/members/{user}"))
    for user in DEVELOPERS:
        ok(api("PUT", f"/teams/{developers}/members/{user}"))
    ensure_repo(PILOT, True)
    # No auto_init: a README in the exceptions store is read back as a record
    # (known issue B2 in docs/SSDLC_UAT_Readiness_Report.pdf).
    ensure_repo(EXCEPTIONS, False)

    out = {}
    if not os.environ.get("WP_OAUTH_DONE"):
        out["GITEA_OAUTH_CLIENT_ID"], out["GITEA_OAUTH_CLIENT_SECRET"] = ensure_oauth(
            "woodpecker", f"{WP}/authorize")
        out["WP_OAUTH_DONE"] = "1"
    if not os.environ.get("PORTAL_OAUTH_CLIENT_ID"):
        out["PORTAL_OAUTH_CLIENT_ID"], out["PORTAL_OAUTH_CLIENT_SECRET"] = ensure_oauth(
            "ssdlc-portal", f"http://{SERVER_IP}:8181/oauth/callback")
    for k, v in out.items():
        print(f"{k}={v}")


def wp_login_session():
    """Full OAuth dance as the admin (docs/adr/0004, 0011); returns a
    requests.Session holding Woodpecker's session cookie."""
    s = requests.Session()
    page = s.get(f"{GITEA}/user/login", timeout=30)
    csrf = re.search(r'name="_csrf"\s+value="([^"]+)"', page.text)
    form = {"user_name": ADMIN, "password": os.environ["ADMIN_PASSWORD"], "remember": "on"}
    if csrf:
        form["_csrf"] = csrf.group(1)
    r = s.post(f"{GITEA}/user/login", data=form, allow_redirects=False, timeout=30)
    if r.status_code not in (302, 303):
        sys.exit(f"ERROR gitea login failed: {r.status_code}")

    r = s.get(f"{WP}/authorize", allow_redirects=False, timeout=30)
    r = s.get(r.headers.get("Location", ""), allow_redirects=False, timeout=30)
    if r.status_code in (302, 303):
        cb = r.headers["Location"]
    else:
        def field(n):
            m = re.search(rf'name="{n}"\s+value="([^"]*)"', r.text)
            return m.group(1) if m else ""
        r = s.post(f"{GITEA}/login/oauth/grant", allow_redirects=False, timeout=30, data={
            "_csrf": field("_csrf"), "client_id": field("client_id"),
            "state": field("state"), "scope": "", "nonce": "",
            "redirect_uri": field("redirect_uri"), "granted": "true"})
        cb = r.headers.get("Location", "")
    if "code=" not in cb:
        sys.exit(f"ERROR oauth grant did not yield a code (got {r.status_code} {cb[:120]})")
    s.get(cb, allow_redirects=False, timeout=30)
    return s


def phase_wp_token():
    s = wp_login_session()
    cfg = s.get(f"{WP}/web-config.js", timeout=30).text
    m = re.search(r'"(eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)"', cfg)
    if not m:
        sys.exit("ERROR no CSRF token in /web-config.js (is the admin logged in?)")
    r = s.post(f"{WP}/api/user/token", headers={"X-CSRF-TOKEN": m.group(1)}, timeout=30)
    if r.status_code != 200 or not r.text.strip():
        sys.exit(f"ERROR minting Woodpecker token: {r.status_code} {r.text[:200]}")
    print(f"WOODPECKER_TOKEN={r.text.strip()}")


def wp(method, path, **kw):
    return requests.request(
        method, f"{WP}/api{path}", timeout=30,
        headers={"Authorization": f"Bearer {os.environ['WOODPECKER_TOKEN']}"}, **kw)


def pilot_repo_id():
    r = wp("GET", f"/repos/lookup/{ORG}/{PILOT}")
    if r.status_code != 200:
        sys.exit(f"ERROR pilot repo not active in Woodpecker: {r.status_code} {r.text[:200]}")
    return r.json()["id"]


def phase_repo_id():
    print(f"PILOT_REPO_ID={pilot_repo_id()}")


def phase_wp_secrets():
    repo_id = pilot_repo_id()
    token = os.environ["REPORTER_TOKEN"]
    for name in ("reviewdog_gitea_token", "policy_eval_gitea_token"):
        body = {"name": name, "value": token, "events": ["push", "pull_request"]}
        r = wp("POST", f"/repos/{repo_id}/secrets", json=body)
        if r.status_code in (409, 500):  # already exists -> update in place
            r = wp("PATCH", f"/repos/{repo_id}/secrets/{name}", json=body)
        if r.status_code >= 300:
            sys.exit(f"ERROR secret {name}: {r.status_code} {r.text[:200]}")


if __name__ == "__main__":
    phases = {"gitea": phase_gitea, "wp-token": phase_wp_token,
              "wp-secrets": phase_wp_secrets, "repo-id": phase_repo_id}
    if len(sys.argv) != 2 or sys.argv[1] not in phases:
        sys.exit("usage: bootstrap.py {" + "|".join(phases) + "}")
    phases[sys.argv[1]]()
