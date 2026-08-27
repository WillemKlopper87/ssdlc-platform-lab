# SADR-0002: Server-side pre-receive secret gate — built and tested

**Status:** Confirmed by experiment
**Date:** 2026-08-23
**Milestone:** 0 (design rework) — resolves D7
**Security & Privacy Impact:** High. This is the only *enforceable* secret control in the design;
pre-commit hooks are convenience, defeated by `--no-verify`.

## Question

D7 states that detecting a secret in CI is not remediating it — by the time Gitleaks fires in a
pipeline, the credential is already in Gitea's object store, webhook payloads, and every clone
taken since. The design calls for a **server-side pre-receive hook** so the objects never land, with
one specific, easy-to-get-wrong rule: the hook must read its suppressions/allowlist **only from the
default branch as it stood before the push**, never from the ref being pushed — otherwise an
attacker ships a real secret and its own allowlist entry in the same push and the hook waves it
through. Both the hook and that specific property needed to be built and proven, not just specified.

## Method

Reused the `compose/forgery-test` harness (Gitea `1.27.2`). Installed the real, pinned
`gitleaks v8.30.1` binary (see [PINNED_VERSIONS.md](../PINNED_VERSIONS.md)) inside the container via
its official release tarball — not a hand-rolled regex approximation. Wrote
`compose/forgery-test/pre-receive-gitleaks.sh` (POSIX `sh`, ~50 lines):

- Reads the git pre-receive protocol (`oldrev newrev refname` per line on stdin).
- Loads the gitleaks allowlist config via `git show <default-branch>:.gitleaks.toml` — a plain git
  object read against the *current* ref, which pre-receive hooks see *before* any ref update, so
  this is correct even when the push target **is** the default branch itself.
- Runs `gitleaks git --log-opts="<oldrev>..<newrev>" --config=<that file> --exit-code=1` scoped to
  only the newly introduced commits (or `<newrev> --not --all` for a new branch).
- Non-zero exit on any finding ⇒ git itself refuses the push before any object is retained.

Installed at `/data/git/repositories/<owner>/<repo>.git/hooks/pre-receive` inside the container
(the real path on this Gitea version — differs from the commonly-assumed
`/data/git/gitea-repositories/...`, corrected empirically after the first attempt 404'd) and made
executable.

**Caveat, stated plainly:** this test replaced Gitea's generated `hooks/pre-receive` outright to
isolate and prove the *scanning logic*. A production install must instead **chain** this script
after Gitea's own generated hook (or via Gitea's supported custom-hooks mechanism, `[git]
DISABLE_GIT_HOOKS = false` plus repo Settings → Git Hooks), so Gitea's own internal checks — LFS,
quota, protocol handling — are not silently skipped. Tracked as a Milestone 1 packaging task, not
re-litigated here since it doesn't change whether the *scanning and suppression logic* is correct.

Three pushes, via real `git push` over Gitea's HTTP protocol with alice's `write:repository`-scoped
token — not simulated:

1. **Real secret, no suppression anywhere.** First attempt used the canonical AWS documentation
   example key (`AKIAIOSFODNN7EXAMPLE`) and correctly found nothing — gitleaks' default ruleset
   allowlists that specific well-known placeholder, which is itself a useful confirmation the tool
   works as shipped. Retried with a realistic, non-example key shape.
2. **The attack this rule exists to stop:** the secret and a self-authored `.gitleaks.toml`
   allowlisting it, bundled in the *same* push.
3. **The legitimate path:** the suppression landed alone first (no secret in that commit, so it
   passes cleanly), then the same secret pushed in a follow-up commit.

## Result

```
1. git push (secret only, no allowlist exists)
   remote: "no .gitleaks.toml on main yet - default gitleaks rules only"
   remote: leaks found: 1
   remote: POLICY: push REJECTED - secret(s) detected on refs/heads/main
   ! [remote rejected] main -> main (pre-receive hook declined)     <- BLOCKED

2. git push (secret + self-authored allowlist, ONE push)
   remote: "no .gitleaks.toml on main yet - default gitleaks rules only"   <- still reads pre-push state
   remote: leaks found: 1
   ! [remote rejected] main -> main (pre-receive hook declined)     <- BLOCKED, bundling didn't help

3a. git push (suppression alone, no secret)
    remote: "no .gitleaks.toml on main yet - default gitleaks rules only"
    remote: no leaks found
    main -> main                                                    <- ACCEPTED

3b. git push (the same secret, now that main carries the allowlist)
    remote: "using allowlist from main:.gitleaks.toml (pre-push state)"    <- now reads it
    remote: no leaks found
    main -> main                                                    <- ACCEPTED
```

**Confirmed, all three properties:**

- A real secret with no suppression is rejected before it ever lands — verified via git's own
  `! [remote rejected]` and non-zero push exit code, not merely a log line.
- **The exact attack D7 was written to prevent does not work.** An attacker cannot ship a secret and
  its own allowlist entry together — the log output itself proves the hook was still reading the
  pre-push state of `main` even on push 2, because pre-receive hooks run before ref updates.
- The legitimate false-positive path — land the suppression first, then push — works exactly as
  designed, and the hook's own log line visibly flips from *"no .gitleaks.toml on main yet"* to
  *"using allowlist from main:.gitleaks.toml"* between pushes 2 and 3b, giving an auditable trace of
  which state was consulted at each point.

## Decision

D7's pre-receive control is **implementable as specified and the suppression-source rule holds**.
Carry forward into Milestone 1:

1. Package `pre-receive-gitleaks.sh` as an Ansible role that **chains after** Gitea's generated hook
   rather than replacing it (the caveat above) — install target: every onboarded repo, via the
   `onboard-repo` script, not a manual per-repo step.
2. The design's `.ssdlc/suppressions.yaml` (rule id, justification, owner, expiry — DESIGN.md
   *Policy model*) is a **different, higher-level mechanism** than the raw `.gitleaks.toml` allowlist
   used in this experiment; this test validates the *retrieval rule* (default-branch-only,
   pre-push state), which applies identically regardless of which file format carries it. Milestone 1
   should decide whether the hook reads `.gitleaks.toml` directly (as here, simplest) or a generated
   subset compiled from `.ssdlc/suppressions.yaml` (matches the rest of the policy model's format).
   Recommend the latter for consistency, at the cost of a small compile step in the hook.
3. Push latency: gitleaks scanned ~110 bytes in 1.2-3.4s in this test, dominated by process startup,
   not data volume — budget for real repo sizes before Milestone 1 signs off on push-time UX; a slow
   pre-receive hook is exactly the kind of friction that gets an admin to disable it.
4. This experiment (or its automated form) becomes a permanent regression test — the three push
   scenarios above are the acceptance criteria for the packaged Ansible role.

## Reproduce it yourself

```
cd compose/forgery-test
docker compose up -d
# wait for http://localhost:3500/api/healthz to return "pass" (took ~50s this run - Alpine +
# fresh sqlite + SSH host key generation; poll with a bounded loop, don't assume a fixed delay)
docker exec forgery-test-gitea sh -c \
  "cd /tmp && wget -q https://github.com/gitleaks/gitleaks/releases/download/v8.30.1/gitleaks_8.30.1_linux_x64.tar.gz \
   && tar xzf gitleaks_8.30.1_linux_x64.tar.gz gitleaks && mv gitleaks /usr/local/bin/ && chmod +x /usr/local/bin/gitleaks"
# create a repo, docker cp pre-receive-gitleaks.sh into
#   /data/git/repositories/<owner>/<repo>.git/hooks/pre-receive , chown git:git, chmod +x
# then push the three scenarios above
docker compose down -v
```

Nothing from a run is meant to persist — `data/` is gitignored. The hook script itself
(`pre-receive-gitleaks.sh`) is the deliverable and IS committed.
