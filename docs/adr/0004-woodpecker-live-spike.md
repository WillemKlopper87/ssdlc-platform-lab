# SADR-0004: Live Woodpecker+Gitea spike — D1 fully confirmed end-to-end

**Status:** Confirmed by experiment, complete
**Date:** 2026-08-24
**Milestone:** 1 (Forge + CI) — completes the D1 spike deferred at the end of Milestone 0
**Security & Privacy Impact:** Low (integration/reliability finding; one real security-relevant
discovery about branch-protection matching, captured in the Decision section).

## Question

[DESIGN.md D1](../DESIGN.md#d1--the-gate-is-a-pipeline-exit-code-not-a-service) confirmed the
exit-code-to-commit-status mechanism against Woodpecker's source but explicitly deferred the live,
end-to-end spike: register a real Gitea OAuth2 app, stand up Woodpecker against it, push a pipeline
with a failing step, and watch its exit code become a Gitea commit status. Deferred at the time
because Woodpecker's login needs an interactive OAuth consent step, and it wasn't obvious that would
reduce to a scripted sequence. This experiment answers that question directly — across two sessions,
three distinct real bugs, and a final green light.

## Environment note

Built during a period of severe host disk pressure (**3.1GB free of 474GB** at the start of the
first session — unrelated to this project, not investigated or remediated here per explicit user
direction to proceed at accepted risk; recovered to ~8.8GB over the course of both sessions from
unrelated activity). Reused already-cached `postgres:16-alpine` rather than pulling the version
named in [PINNED_VERSIONS.md](../PINNED_VERSIONS.md) to conserve headroom; re-pin explicitly once
disk headroom allows re-verification — flagged in `compose/minimal/docker-compose.yml` itself.

## Method

Built `compose/minimal/docker-compose.yml` — Gitea `1.27.2`, Woodpecker server + agent `v3.17.0`,
shared Postgres. Registered a real Gitea OAuth2 application via the API
(`POST /user/applications/oauth2`, `confidential_client: true`) rather than assuming one existed,
and scripted the full interactive OAuth2 consent flow end-to-end with curl and cookie jars — no
browser, across every attempt in both sessions:

1. `POST /user/login` with a session cookie jar.
2. `GET /authorize` on **Woodpecker itself** with no `state` fabricated — Woodpecker generates its
   own signed JWT `state` and redirects to Gitea. (A fabricated `state` was tried once, deliberately,
   as a forgery check — Woodpecker correctly rejected it: `"cannot verify state token" error="token
   is malformed"`, since its `state` is a real signed JWT, not an opaque nonce. An unplanned but
   relevant confirmation that Woodpecker resists exactly the class of forgery this whole project
   guards against.)
3. Presented Gitea's real HTML consent page (`Authorize Application` button, POSTing to
   `/login/oauth/grant`) and submitted it.
4. Fed Gitea's resulting code back to Woodpecker's `/authorize` callback (same endpoint, dual
   purpose — confirmed from source).

Login itself worked on the very first attempt, both sessions. Everything past login took three
rounds of real debugging to get through.

## Three real bugs found and fixed, in the order encountered

### 1. Internal-vs-external Gitea URL split (session 1, partially fixed)

Every forge-dependent Woodpecker call 401'd. Root-caused via source review (ruled out CSRF cookies,
clock skew, and the obvious middleware paths at the time — later understood to be incomplete, see
bug 3) and corroborated against [GitHub issue
#1560](https://github.com/woodpecker-ci/woodpecker/issues/1560), a real, still-open Woodpecker
limitation around using different internal (`gitea:3000`) vs. external (`localhost:3500`) URLs for
the same forge on same-host Docker deployments.

**Fix:** `network_mode: "service:gitea"` on `woodpecker-server`, with Gitea's `HTTP_PORT` set
directly to `3500` (not the usual `3000→3500` mapping), so `localhost:3500` resolves identically
whether the caller is the host, Woodpecker, or a browser. Woodpecker's own ports (8000 HTTP, 9000
gRPC) moved to Gitea's `ports:` block, since a container sharing another's network namespace has none
of its own to publish from. This is good, standard Docker Compose practice and was kept even after
bug 3 turned out to be the *actual* cause of the original 401s — the URL split is still real, still
documented upstream, and this fix still removes it as a variable.

### 2. Gitea's outbound-webhook SSRF protection (session 2)

With the network fix in place and the CSRF fix (below) also applied, the repo activated and Gitea
attempted to deliver its webhook to Woodpecker — and refused, on purpose:

```
services/webhook/webhook.go:99:handler() [E] Unable to deliver webhook task[1] ...
dial tcp [::1]:8000: webhook can only call allowed HTTP servers
(check your security.ALLOWED_HOST_LIST setting), deny 'localhost([::1]:8000)'
```

Gitea blocks outbound webhooks to loopback/private addresses by default — a real SSRF-prevention
control, and a good one; framework-relevant, not a bug to route around carelessly. **Fix:**
`GITEA__security__ALLOWED_HOST_LIST=loopback,private`, added with an explicit comment that this is a
same-host **dev/test** allowance — production should scope this to real internal service ranges, not
blanket loopback, matching D6's "explicit, not wildcard" posture elsewhere in this design.

### 3. IPv6/IPv4 localhost resolution (session 2) — plus the real root cause of bug-1's symptom

Fixing bug 2 exposed a second delivery failure: `dial tcp [::1]:8000: connect: connection refused`.
Gitea resolves `localhost` to IPv6 `::1` first; Woodpecker's listeners are IPv4-only. **Fix:**
replaced every `localhost` with explicit `127.0.0.1` throughout the compose file and the registered
OAuth app's redirect URI (which required registering a fresh OAuth2 application, since Gitea
validates `redirect_uri` against exactly what was registered).

**While root-causing this, the actual explanation for session 1's original 401s finally surfaced**,
and it was not the URL split at all. Direct evidence: the stored Gitea access token, when replayed
by hand straight at Gitea's API (`GET /api/v1/user`, `GET /api/v1/user/repos`), worked perfectly —
`200 OK` every time. The token was never the problem. Reading Woodpecker's raw source directly
(bypassing an AI-summarized fetch that had glossed over it) found the true cause in
`server/router/middleware/session/user.go`:

```go
if t.Type == token.SessToken {
    err = token.CheckCsrf(c.Request, func(_ *token.Token) (string, error) {
        return user.Hash, nil
    })
    if err != nil {
        c.AbortWithStatus(http.StatusUnauthorized)   // empty body — matches every 401 observed
        return
    }
}
```

and in `shared/token/token.go`:

```go
func CheckCsrf(r *http.Request, fn SecretFunc) error {
    switch r.Method {
    case http.MethodGet, http.MethodOptions:
        return nil   // explains why GET /api/user always worked
    }
    raw := r.Header.Get("X-CSRF-TOKEN")
    _, err := Parse([]Type{CsrfToken}, raw, fn)
    return err
}
```

Every session-cookie-authenticated **write** requires an `X-CSRF-TOKEN` header: a JWT of type
`csrf`, HS256-signed with the user's `hash` column as the secret. Nothing in Woodpecker's docs
surfaces this for API scripting — it's designed for the SPA frontend to handle silently. Read the
signing secret directly from Woodpecker's own Postgres (`SELECT hash FROM users WHERE id=1`) and
constructed a matching JWT by hand (Python, `hmac`+`hashlib`+`base64`, no library):

```
POST /api/user/repos/refresh   (no X-CSRF-TOKEN)  → 401, empty body
POST /api/user/repos/refresh   (+ X-CSRF-TOKEN)   → 200 "Ok"
```

**This was the actual root cause all along.** The network-namespace fix (bug 1) is still correct,
still worth keeping, and still fixes a real, independently-documented upstream limitation — but it
was not what was blocking this specific symptom. Both true; neither redundant.

## Result — the full loop, live

With all three fixes in place: created a repo, activated it in Woodpecker (`POST /api/repos
?forge_remote_id=<id>`, with the CSRF header), confirmed Woodpecker auto-registered its Gitea webhook
pointed at `127.0.0.1:8000` (correct, per fix 3), then pushed a real `.woodpecker.yml`:

```yaml
steps:
  fail-on-purpose:
    image: alpine:3
    commands:
      - "echo simulating policy-eval - a finding was Critical"
      - exit 1
```

(First push used an unquoted string containing a colon — `"a finding was Critical:"` style — which
YAML's flow-mapping parser misread as a nested map. A pure authoring mistake, not a platform issue,
and itself a small useful reminder: quote pipeline step strings that contain colons.)

```
git push  →  Gitea webhook fires, no error logged (silence = success at this log level)
Woodpecker: pipeline #2, status "running" → status "failure"
GET /api/v1/repos/gateadmin/gate-test/commits/<sha>/status
  → "state": "failure", "total_count": 1
  → statuses[0]:
       "status": "failure"
       "context": "ssdlc/security-gate/push/woodpecker"
       "description": "Pipeline failed"
       "target_url": "http://127.0.0.1:8000/repos/1/pipeline/2/1"
       "creator": "gateadmin"   (the Woodpecker/Gitea integration, not a human)
```

**Confirmed, completely.** A pipeline step's `exit 1` became a Gitea commit status of `failure`,
reported natively by Woodpecker with no custom code in the trust path — exactly D1's design claim,
now proven against a real push, a real webhook, a real agent execution, and a real status write.

## Decision

D1 is closed. Three items carry forward:

1. **The status context is not the bare string originally assumed.** `WOODPECKER_STATUS_CONTEXT=
   ssdlc/security-gate` produced `ssdlc/security-gate/push/woodpecker` — Woodpecker's context
   template appends `/{event}/{workflow}`. A PR-triggered pipeline will produce
   `ssdlc/security-gate/pull_request/woodpecker` instead — a **different** string. Milestone 3's
   branch protection must use a **glob pattern** (`ssdlc/security-gate/**` or `ssdlc/security-gate/*`
   — Gitea's `status_check_contexts` supports glob matching per its own docs) rather than the exact
   string this design originally assumed throughout. This is a real correction to D1/D2's examples,
   not just this SADR — update the literal `status_check_contexts` value wherever it's referenced
   before Milestone 3 configures real branch protection.
2. Fold fixes 1–3 into the Ansible role that provisions Gitea+Woodpecker for real (Milestone 1
   remaining work): network-namespace sharing (or an equivalent reverse-proxy unification),
   `ALLOWED_HOST_LIST` scoped appropriately per environment (loopback+private for dev, real internal
   ranges for anything further), IPv4-explicit addressing, and `WOODPECKER_GRPC_SECRET` set and
   persisted (noticed as a side effect of an earlier restart silently invalidating an in-progress
   session — still worth fixing even though it wasn't the main blocker).
3. **The CSRF-token construction is not something the platform's own tooling should ever need to
   repeat.** Real API/CLI automation (Milestone 3's sidecar, `onboard-repo.sh`, etc.) should either
   use Woodpecker's documented CLI (`woodpecker-cli`, which presumably handles this correctly) or a
   proper Personal Access Token obtained once through the UI — not hand-rolled JWT construction
   against a secret read from the database. That approach was legitimate and necessary *for this
   diagnostic experiment*, where the entire point was proving the mechanism from first principles,
   but it is not the pattern for production automation.

## Reproduce it yourself

```
cd compose/minimal
cp .env.example .env   # POSTGRES_PASSWORD, WOODPECKER_AGENT_SECRET, WOODPECKER_GRPC_SECRET
                        # (openssl rand -hex 32 for the latter two)
docker compose up -d postgres
# wait for postgres healthy, then:
docker compose up -d gitea
# wait for http://127.0.0.1:3500/api/healthz == "pass", then create an admin user,
# mint a token, POST /api/v1/user/applications/oauth2 with
# redirect_uris=["http://127.0.0.1:8000/authorize"], confidential_client=true,
# put client_id/secret into .env
docker compose up -d woodpecker-server woodpecker-agent
# log in (see Method above), activate a repo (POST /api/repos?forge_remote_id=<id>,
# with a hand-built X-CSRF-TOKEN — see the source excerpt above for the exact algorithm),
# push a .woodpecker.yml with a failing step, check the resulting commit status
docker compose down -v
```
