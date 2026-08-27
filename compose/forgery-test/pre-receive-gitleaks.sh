#!/bin/sh
# Server-side pre-receive secret gate (SSDLC-Platform-Lab, D7).
#
# Runs INSIDE the bare repo at push time, before any ref is updated, so
# rejected objects never land. The allowlist config is read from the
# DEFAULT BRANCH — never from the ref being pushed — because reading it
# from the incoming ref would let an attacker ship a real secret alongside
# its own allowlist entry in the same push. Since this hook runs before
# refs update, `git show <default-branch>:.gitleaks.toml` is naturally the
# already-accepted version even when the push target IS the default branch.

set -eu
FAIL=0
DEFAULT_BRANCH="${SSDLC_DEFAULT_BRANCH:-main}"

CONFIG_TMP=$(mktemp)
if git show "${DEFAULT_BRANCH}:.gitleaks.toml" > "$CONFIG_TMP" 2>/dev/null; then
  CONFIG_ARGS="--config=$CONFIG_TMP"
  echo "pre-receive: using allowlist from ${DEFAULT_BRANCH}:.gitleaks.toml (pre-push state)" >&2
else
  CONFIG_ARGS=""
  echo "pre-receive: no .gitleaks.toml on ${DEFAULT_BRANCH} yet — default gitleaks rules only" >&2
fi

while read -r oldrev newrev refname; do
  case "$newrev" in
    0000000000000000000000000000000000000000) continue ;;
  esac

  if [ "$oldrev" = "0000000000000000000000000000000000000000" ]; then
    LOGOPTS="${newrev} --not --all"
  else
    LOGOPTS="${oldrev}..${newrev}"
  fi

  REPORT=$(mktemp)
  if ! gitleaks git --log-opts="$LOGOPTS" $CONFIG_ARGS \
        --report-format=json --report-path="$REPORT" --exit-code=1 --no-banner . ; then
    echo "" >&2
    echo "POLICY: push REJECTED — secret(s) detected on ${refname}" >&2
    echo "        Objects were not accepted. Rotate any real credential now." >&2
    echo "        False positive? Land a suppression PR on ${DEFAULT_BRANCH} FIRST" >&2
    echo "        (it contains no secret, so it passes), then push this again." >&2
    FAIL=1
  fi
  rm -f "$REPORT"
done

rm -f "$CONFIG_TMP"
exit "$FAIL"
