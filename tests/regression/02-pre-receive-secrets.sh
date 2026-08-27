# tests/regression/02-pre-receive-secrets.sh — SADR-0002, automated.
#
# Three real pushes: (1) a bare secret, no suppression anywhere -- must be
# rejected before it ever lands; (2) the secret AND a self-authored
# allowlist for it, bundled in the SAME push -- must still be rejected,
# because pre-receive reads the suppression state as it stood on the
# default branch BEFORE this push, never from the incoming ref; (3) the
# legitimate path -- suppression lands alone first (accepted, no secret in
# that commit), then the same secret in a follow-up push (now accepted,
# because the suppression already exists on the default branch).
#
# Requires GITEA, GITEA_ADMIN_TOKEN, FORGERY_CONTAINER already set.
#
# The fixture key (AKIAZXK4ZWL2XFXOZ7BQ) is deliberately NOT
# AKIAIOSFODNN7EXAMPLE -- docs/adr/0002 already found gitleaks' default
# ruleset allowlists that exact canonical placeholder. Found live on this
# suite's own first run that an arbitrary all-repeated-character fake
# ("AKIA3ZZZZZZZZZZZZZZZ") also produces zero findings -- close enough to
# the right length/prefix to look plausible by eye, not close enough to
# whatever gitleaks' actual pattern/entropy check wants. Confirmed this
# one triggers a real finding before relying on it here.

test_pre_receive_secrets() {
  test_start "SADR-0002: pre-receive secret gate"

  gitea_create_user "$GITEA" "$FORGERY_CONTAINER" alice-02 AlicePw123! alice-02@example.com
  local alice_token
  alice_token=$(gitea_mint_token "$GITEA" alice-02 'AlicePw123!' alice-02-token '["write:repository"]')

  curl -s -X POST "${GITEA}/api/v1/user/repos" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
    -H "Content-Type: application/json" -d '{"name":"secret-gate-02","auto_init":true,"default_branch":"main"}' >/dev/null
  curl -s -X PUT "${GITEA}/api/v1/repos/gateadmin/secret-gate-02/collaborators/alice-02" \
    -H "Authorization: token ${GITEA_ADMIN_TOKEN}" -H "Content-Type: application/json" -d '{"permission":"write"}' >/dev/null

  # Install the real, pinned gitleaks binary inside the container --
  # matching docs/adr/0002 exactly, not a regex approximation.
  docker exec "$FORGERY_CONTAINER" sh -c \
    "cd /tmp && wget -q https://github.com/gitleaks/gitleaks/releases/download/v8.30.1/gitleaks_8.30.1_linux_x64.tar.gz \
     && tar xzf gitleaks_8.30.1_linux_x64.tar.gz gitleaks && mv gitleaks /usr/local/bin/ && chmod +x /usr/local/bin/gitleaks" \
    >/dev/null 2>&1

  local hook_script hook_path hook_script_win
  hook_script="${SCRIPT_DIR}/../../compose/forgery-test/pre-receive-gitleaks.sh"
  hook_path="/data/git/repositories/gateadmin/secret-gate-02.git/hooks/pre-receive"
  # MSYS_NO_PATHCONV=1 scoped to just these three calls -- git-bash on
  # Windows rewrites the CONTAINER-side absolute path into a bogus host
  # path otherwise (C:/Program Files/Git/data/git/...), found live here.
  # Deliberately not exported for the whole script: doing that broke
  # git_push_feature_branch's own `mktemp -d` + `cd` elsewhere in this
  # suite (a *real*, host-side temp path that needs normal conversion) --
  # the fix has to be scoped to the specific docker calls that need it,
  # not blanket-applied.
  #
  # `docker cp` makes this harder still: it takes a HOST path and a
  # CONTAINER path in the SAME invocation, so MSYS_NO_PATHCONV=1 protects
  # the container-side target but then breaks the host-side source path
  # too (found live: "GetFileAttributesEx C:\c\applications: ... cannot
  # find the file" -- the source path was left as a raw POSIX string with
  # no conversion at all). Fix: pre-resolve the host path to native
  # Windows form with `cygpath -w` before the MSYS_NO_PATHCONV=1 call, so
  # it needs no conversion either way.
  hook_script_win=$(cygpath -w "$hook_script")
  MSYS_NO_PATHCONV=1 docker cp "$hook_script_win" "${FORGERY_CONTAINER}:${hook_path}" >/dev/null
  MSYS_NO_PATHCONV=1 docker exec "$FORGERY_CONTAINER" chown git:git "$hook_path" >/dev/null
  MSYS_NO_PATHCONV=1 docker exec "$FORGERY_CONTAINER" chmod +x "$hook_path" >/dev/null

  local work_dir push_url
  work_dir=$(mktemp -d)
  git clone -q "${GITEA}/gateadmin/secret-gate-02.git" "$work_dir" 2>/dev/null
  (
    cd "$work_dir"
    git config user.email alice-02@example.com
    git config user.name alice-02

    # --- Scenario 1: bare secret, no suppression anywhere ---
    echo 'AWS_KEY="AKIAZXK4ZWL2XFXOZ7BQ"' > secret1.txt
    git add secret1.txt
    git commit -q -m "add secret, no allowlist"
    if git -c http.extraHeader="Authorization: token ${alice_token}" push -q origin main >/tmp/push1.log 2>&1; then
      echo "SCENARIO1_RESULT=accepted"
    else
      echo "SCENARIO1_RESULT=rejected"
    fi
    git reset -q --hard HEAD~1

    # --- Scenario 2: secret + self-authored allowlist, SAME push ---
    echo 'AWS_KEY="AKIAZXK4ZWL2XFXOZ7BQ"' > secret2.txt
    cat > .gitleaks.toml <<'TOML'
[allowlist]
regexes = ['''AKIAZXK4ZWL2XFXOZ7BQ''']
TOML
    git add secret2.txt .gitleaks.toml
    git commit -q -m "secret + self-authored allowlist, one push"
    if git -c http.extraHeader="Authorization: token ${alice_token}" push -q origin main >/tmp/push2.log 2>&1; then
      echo "SCENARIO2_RESULT=accepted"
    else
      echo "SCENARIO2_RESULT=rejected"
    fi
    git reset -q --hard HEAD~1

    # --- Scenario 3a: suppression alone, no secret ---
    cat > .gitleaks.toml <<'TOML'
[allowlist]
regexes = ['''AKIAZXK4ZWL2XFXOZ7BQ''']
TOML
    git add .gitleaks.toml
    git commit -q -m "land the suppression alone first"
    if git -c http.extraHeader="Authorization: token ${alice_token}" push -q origin main >/tmp/push3a.log 2>&1; then
      echo "SCENARIO3A_RESULT=accepted"
    else
      echo "SCENARIO3A_RESULT=rejected"
    fi

    # --- Scenario 3b: same secret, now that main carries the allowlist ---
    echo 'AWS_KEY="AKIAZXK4ZWL2XFXOZ7BQ"' > secret3.txt
    git add secret3.txt
    git commit -q -m "same secret, follow-up push"
    if git -c http.extraHeader="Authorization: token ${alice_token}" push -q origin main >/tmp/push3b.log 2>&1; then
      echo "SCENARIO3B_RESULT=accepted"
    else
      echo "SCENARIO3B_RESULT=rejected"
    fi
  ) > /tmp/pre-receive-results.txt
  rm -rf "$work_dir"

  assert_eq "$(grep SCENARIO1_RESULT /tmp/pre-receive-results.txt | cut -d= -f2)" "rejected" \
    "bare secret with no suppression is rejected before landing"
  assert_eq "$(grep SCENARIO2_RESULT /tmp/pre-receive-results.txt | cut -d= -f2)" "rejected" \
    "secret + self-authored allowlist in the SAME push is still rejected"
  assert_eq "$(grep SCENARIO3A_RESULT /tmp/pre-receive-results.txt | cut -d= -f2)" "accepted" \
    "suppression alone (no secret) is accepted"
  assert_eq "$(grep SCENARIO3B_RESULT /tmp/pre-receive-results.txt | cut -d= -f2)" "accepted" \
    "same secret accepted once the suppression already exists on main"

  rm -f /tmp/push1.log /tmp/push2.log /tmp/push3a.log /tmp/push3b.log /tmp/pre-receive-results.txt
}
