# Security guide for platform users

This lab is a teaching and pilot environment, not a production security
service. Do not put production credentials, customer data, or security
incidents in a test repository or pipeline log.

## If a gate fails

1. Open the pull-request check output and find the first blocking finding.
   The platform blocks Critical and High findings; Medium and Low findings are
   visible for learning and prioritisation.
2. Fix the code, dependency, secret, or infrastructure definition and push a
   new commit. Reviews are intentionally stale after a new commit, so request
   a fresh human review once the check is green.
3. If the result is a false positive or remediation cannot happen now, do not
   weaken the pipeline. Use the framework's two-person risk-acceptance process
   and record the scope, expiry date, owner, and compensating control.
4. If a credential may have been committed, revoke or rotate it immediately.
   Removing it from the next commit does not make the old value safe; it may
   already exist in Git history, build logs, or clones.

## Reporting a platform weakness

Do not create a public issue for a bypass, exposed token, or suspected
compromise. Report it directly to the lab/platform owner with the repository,
commit or PR link, observed behaviour, and any safe reproduction steps. The
owner should revoke affected tokens, preserve relevant logs, and create a
tracked remediation record.

## Local early feedback (optional but recommended)

Install the pinned `pre-commit` tool, then run:

```sh
pre-commit install
pre-commit run --all-files
```

The configuration runs a fast secret scan and basic file checks before a
commit. It improves feedback speed; the server-side gate remains authoritative.
Versions are recorded in [docs/PINNED_VERSIONS.md](docs/PINNED_VERSIONS.md).
