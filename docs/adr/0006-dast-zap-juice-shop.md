# SADR-0006: DAST leg closed — ZAP baseline scan proven against a live target

**Status:** Confirmed by experiment
**Date:** 2026-08-24
**Milestone:** 1 (Forge + CI) — closes the DAST gap flagged as outstanding at the end of SADR-0005
**Security & Privacy Impact:** Low. Confirms the scanning mechanism works; does not change any
design decision. One real finding worth carrying forward: ZAP's baseline scan genuinely produces
`WARN`, not `FAIL`, by default — corroborating DESIGN.md's own stated placement rather than
contradicting it.

## Question

SADR-0005 proved SCA (Trivy) live, end-to-end, through the real onboarding automation, and
explicitly named the DAST leg (ZAP + a vulnerable target) as the still-outstanding half of "go build
and live-test SCA and DAST." This experiment answers it directly: does OWASP ZAP's baseline scan,
run against a real running (deliberately vulnerable) web application, produce real findings — and
what does that scan actually look like in practice, since DAST hadn't been demonstrated at all yet
in this project.

## Method

Target: **OWASP Juice Shop**, `bkimminich/juice-shop:v20.2.0` — the standard, actively maintained
open-source deliberately-vulnerable application used industry-wide for exactly this purpose.
Scanner: `zaproxy/zap-stable:latest` (the current correct image — see the naming note below).

```
docker run -d --name ssdlc-juice-shop -p 3300:3000 bkimminich/juice-shop:v20.2.0
# responsive immediately (http://127.0.0.1:3300 -> 200)

docker network create ssdlc-dast-test
docker network connect ssdlc-dast-test ssdlc-juice-shop

docker run --rm --network ssdlc-dast-test \
  -v "<scratch-dir>:/zap/wrk/:rw" \
  zaproxy/zap-stable zap-baseline.py \
  -t http://ssdlc-juice-shop:3000 \
  -J zap-report.json -r zap-report.html
```

Both containers were put on a dedicated bridge network with ZAP addressing Juice Shop by container
name — the same pattern SADR-0005 established for the Woodpecker agent's step containers, applied
here rather than re-derived from scratch.

## Two real environment issues found and fixed, both cheap once identified

### 1. `zaproxy/zaproxy:stable` does not exist — the correct image moved

`docker manifest inspect zaproxy/zaproxy:2.17.0` and `:stable` both returned
`denied: requested access to the resource is denied`, which reads like an auth problem but isn't
one. The actual repository is **`zaproxy/zap-stable`** (or `ghcr.io/zaproxy/zaproxy:stable`) — the
older `owasp/zap2docker-stable` is deprecated, and the naming pattern people expect
(`zaproxy/zaproxy:stable`) is not how the current images are actually published.
[PINNED_VERSIONS.md](../PINNED_VERSIONS.md)'s `zaproxy/zaproxy` reference needs correcting to
`zaproxy/zap-stable` — tracked as a follow-up, not fixed in that file as part of this SADR to keep
the two changes separately reviewable.

### 2. git-bash volume-mount path mangling

The first scan attempt failed immediately: `A file based option has been specified but the
directory '/zap/wrk' is not mounted`. Root cause: git-bash on Windows rewrites `/c/...`-style paths
before Docker ever sees them, and for a `-v host:container` argument this rewriting can mangle the
colon-separated pair. **Fix:** `export MSYS_NO_PATHCONV=1` before the `docker run` — the standard,
well-known fix for this exact class of problem, not previously needed in this project because no
earlier experiment used a bind-mounted volume from a git-bash shell.

## Result

```
FAIL-NEW: 0   FAIL-INPROG: 0   WARN-NEW: 8   WARN-INPROG: 0   INFO: 0   IGNORE: 0   PASS: 59
```

Structured JSON report, parsed directly (not just the console summary):

```
site: http://ssdlc-juice-shop:3000   alerts: 11
  - Content Security Policy (CSP) Header Not Set        [Medium/High]   5 instances
  - Cross-Domain Misconfiguration                        [Medium/Medium] 5 instances
  - Cross-Origin-Embedder-Policy Header Missing/Invalid  [Low/Medium]    5 instances
  - Cross-Origin-Opener-Policy Header Missing/Invalid    [Low/Medium]    5 instances
  - Dangerous JS Functions                               [Low/Low]       1 instance
  - Deprecated Feature Policy Header Set                 [Low/Medium]    5 instances
  - Timestamp Disclosure - Unix                          [Low/Low]       5 instances
  - Modern Web Application                               [Info/Medium]   5 instances
  - Non-Storable Content                                 [Info/Medium]   4 instances
  - Storable and Cacheable Content                       [Info/Medium]   1 instance
  - Storable but Non-Cacheable Content                   [Info/Medium]   5 instances
```

**Confirmed.** A real scan against a real running target produced real, risk-classified findings —
missing security headers, cache-control misconfigurations, a dangerous-JS-function pattern match on
`main.js` — with a full HTML report (`zap-report.html`, 98 KB) and machine-readable JSON
(`zap-report.json`, 37 KB) as artifacts. `zap-baseline.py`'s own `--auto` mode additionally generated
`zap.yaml`, an Automation Framework plan file — worth knowing about for Milestone 6, since it's the
mechanism for scripting more elaborate authenticated scans later rather than hand-building one.

**Confirms, rather than contradicts, DESIGN.md's placement decision.** Every finding landed as
`WARN`, none as `FAIL` — this is ZAP baseline's actual default behaviour on a deliberately vulnerable
target, not a tooling shortfall. It corroborates framework §3.2's own specification: DAST runs
**warn-mode at pre-prod staging**, with "a documented path to blocking once thresholds & false-positive
handling mature" (DESIGN.md, Deployment profiles section). Juice Shop's genuinely serious
vulnerabilities (SQLi, broken auth, IDOR, etc. — the reason it's used for OWASP Top Ten training)
are **not** what ZAP's passive baseline scan surfaces; those need the active/authenticated scan modes
this experiment deliberately did not exercise, matching the "baseline" scope named in the design.

## Decision

1. **DAST is proven live** — the second half of "go build and live-test SCA and DAST" is complete.
   Combined with [SADR-0005](0005-sca-dast-automation.md), both scanning legs named in that request
   now have real evidence, not just a design description.
2. **Correct `PINNED_VERSIONS.md`**: `zaproxy/zaproxy` → `zaproxy/zap-stable`. Tracked as a small,
   separate follow-up edit.
3. **Not yet wired into automation.** Unlike SCA (which runs on every push via `onboard-repo.sh`'s
   pipeline template), this scan was run manually against a standalone target — it has not yet been
   made a *scheduled* Woodpecker pipeline against a real staging environment, which is where
   DESIGN.md's Deployment profiles section places it. That wiring is the next piece, not done here:
   this SADR proves the scanning mechanism works; it does not yet prove the automation of running it
   on a schedule without human interaction.
4. **Baseline-only was the right first cut.** Confirming WARN-not-FAIL as the honest default supports
   DESIGN.md's own staged rollout (warn now, define a blocking threshold once false-positive behaviour
   is understood) rather than jumping straight to enforcement on an untuned DAST signal — the same
   principle Milestone 2's "no blocking, measure the false-positive rate first" already applies to
   the fast gate.
5. Add `MSYS_NO_PATHCONV=1` to any future script in this repo that does a `docker run -v` from a
   git-bash context — the first place this will matter for real is the eventual scheduled DAST
   pipeline definition itself.

## Reproduce it yourself

```
docker run -d --name ssdlc-juice-shop -p 3300:3000 bkimminich/juice-shop:v20.2.0
docker network create ssdlc-dast-test
docker network connect ssdlc-dast-test ssdlc-juice-shop

export MSYS_NO_PATHCONV=1   # only needed from a git-bash / Windows shell
mkdir -p /tmp/dast-report && cd /tmp/dast-report
docker run --rm --network ssdlc-dast-test -v "$(pwd):/zap/wrk/:rw" \
  zaproxy/zap-stable zap-baseline.py -t http://ssdlc-juice-shop:3000 \
  -J zap-report.json -r zap-report.html

docker rm -f ssdlc-juice-shop
docker network rm ssdlc-dast-test
```

Nothing here is meant to persist — the target and scanner are both throwaway, torn down after the
run, same as every prior experiment in this series.
