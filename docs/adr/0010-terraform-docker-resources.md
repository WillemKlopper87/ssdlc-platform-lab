# SADR-0010: First IaC slice — Terraform for the docker-level resources, live-tested; Ansible written but blocked from execution on this host; a real Checkov coverage gap found along the way

**Status:** Terraform confirmed by experiment (full apply/destroy lifecycle, independently verified
against the Docker daemon directly). Ansible written, grounded in already-proven commands, but **not
executed** — a genuine environment constraint, not a shortcut. Checkov run against the new Terraform:
succeeded, but found to test nothing meaningful for these specific resource types.
**Date:** 2026-08-24
**Milestone:** 1/6 (Forge + CI / Deploy + runtime) — first artifact in `terraform/` and `ansible/`,
both empty before this
**Security & Privacy Impact:** Low directly (no change to the gate or trust path). Indirectly
relevant to D6 (self-defence/dogfooding) and to the honesty of any future "our IaC passes Checkov"
claim.

## Question

DESIGN.md's repository structure names `terraform/` ("hosts, networks, volumes, K8s"), `ansible/`
("playbooks 00-base … 50-runtime, roles/"), and `kubernetes/` as real parts of the platform. All
three were empty directories — nothing built, nothing tested. This was flagged as a critical gap.
The question: what is the first slice of this that can actually be built **and proven**, rather than
just designed, given the tools and target actually available in this development environment?

## Method — finding out what's actually executable here, not assuming

**Ansible: tried directly, not assumed to work.** `pip install ansible-core` succeeds cleanly on
native Windows. Running it does not:

```
$ ansible-playbook --version
Traceback (most recent call last):
  ...
  File "...\ansible\cli\__init__.py", line 46, in check_blocking_io
    if not os.get_blocking(fd):
OSError: [WinError 87] The parameter is incorrect
```

`os.get_blocking()` is unsupported on Windows file descriptors — this is Ansible's own control-node
platform requirement (Linux/macOS/WSL only), not a missing dependency. Confirmed the toolchain
problem is not limited to `ansible-playbook` itself: `ansible-lint` was also tried, and fails
differently but just as fundamentally (`CRITICAL:root:No module named 'grp'` — another POSIX-only
stdlib module Ansible's internals import unconditionally). Two independent tools, two independent
POSIX-only failure modes. The only WSL distro present on this machine is Docker Desktop's own
internal one — not an appropriate place to install arbitrary tooling for this. Raised to the user
directly rather than silently choosing a workaround; **decision: proceed with Terraform first (which
does run natively here), write Ansible carefully against already-proven commands, and mark it
explicitly unexecuted** rather than either fabricating a live test or blocking entirely.

**Terraform: tried directly, and it works.** Downloaded the Windows binary directly (not through a
system package manager, since this host has no assumed admin-elevated install path) —
`terraform_1.15.9_windows_amd64.zip` from HashiCorp's own release server, matching the latest
version confirmed live via `gh api repos/hashicorp/terraform/releases/latest`. Runs natively, no
POSIX dependency.

**Checkov: installed and run, but the result needed a second look before trusting it.** `checkov -d
terraform/local --framework terraform` exited 0 with an empty result (`resource_count: 0`) — not the
"passed, N checks" summary a clean scan normally produces. Ruled out the obvious false explanations
before accepting the real one: re-ran against a plain `aws_s3_bucket` resource in a scratch directory
(found 1 resource, 4 passed / 7 failed checks — the tool itself works); re-ran against a copy of the
same `.tf` files with `.terraform/`/`terraform.tfstate` removed (still 0). Then checked directly:

```
grep -rl "docker_network\|docker_volume" <checkov-package>/terraform/checks
-> 0 files
```

**Checkov ships zero built-in Terraform policies for the `docker` provider's resource types.** A
clean Checkov run against `docker_network`/`docker_volume` resources proves nothing about their
security — it proves only that Checkov has no opinion on them. This matters directly for D6's "the
platform's own Compose/IaC passes Checkov" claim: that claim is not meaningfully validated by this
file set. It becomes meaningful once IaC exists for a target Checkov actually has policies for —
cloud VM/network/firewall resources (once a real `terraform/<provider>/` host module exists), or
Kubernetes manifests/Dockerfiles, both of which Checkov does cover.

## What was built

**`terraform/local/`** — the docker-daemon-level slice DESIGN.md's "networks, volumes" covers,
scoped deliberately short of "hosts" (real cloud VM provisioning, which needs a real provider and
credentials this environment doesn't have — untested cloud HCL doesn't belong in this repo per its
own live-test discipline). Manages the network and three named volumes that
`compose/minimal/docker-compose.yml` currently creates implicitly, with real labels
(`ssdlc.profile`, `ssdlc.component`) — the same resources, made declarative and reviewable instead of
implicit compose side effects.

Discovered along the way: the `kreuzwerker/docker` provider's built-in default host
(`npipe:////./pipe/docker_engine`) is **not** correct for this machine's actual Docker Desktop
install — confirmed via `docker context inspect`, the real endpoint is
`npipe:////./pipe/dockerDesktopLinuxEngine`. Exposed as a variable with the failure documented
directly in its description, rather than silently hardcoded.

**Full lifecycle live-tested, each step independently verified against `docker` directly, not just
trusted from Terraform's own output:**

```
terraform init    -> kreuzwerker/docker v4.5.0 installed
terraform plan    -> 4 to add
terraform apply   -> 4 added
  docker network ls --filter name=ssdlc-minimal   -> ssdlc-minimal, bridge, local
  docker network inspect ... --format '{{json .Labels}}'
    -> {"ssdlc.managed-by":"terraform","ssdlc.profile":"minimal"}
  docker volume ls --filter name=ssdlc-minimal
    -> ssdlc-minimal-gitea-data, ssdlc-minimal-postgres-data, ssdlc-minimal-woodpecker-server-data
terraform plan (again)     -> "No changes" (idempotency confirmed)
terraform destroy          -> 4 destroyed
  docker network ls / docker volume ls --filter name=ssdlc-minimal   -> both empty (cleanup confirmed)
```

**Not yet wired into `compose/minimal/docker-compose.yml`** as `external: true` resources —
that changeover needs a full re-verification pass of the already-tested minimal stack (bring
postgres/gitea/woodpecker back up, re-confirm OAuth still resolves, re-run the SADR-0004/0008/0009
checks) rather than being folded into this change unverified. Tracked below, not done here.

**`ansible/`** — `site.yml` plus `00-base.yml` … `50-runtime.yml`, automating the exact manual
bring-up sequence SADR-0004 proved by hand and SADR-0004's own decision item 2 explicitly asked for
("Fold fixes 1–3 into the Ansible role that provisions Gitea+Woodpecker for real"). Every individual
task is grounded in a command already proven live in this project:

- Gitea's `/api/healthz` shape (`{"status": "pass", ...}`) — SADR-0004 and SADR-0009.
- `gitea admin user create` / `gitea admin user list` via `docker exec` — SADR-0009's forgery-test
  work, same Gitea image.
- Admin token minting (`POST /users/{user}/tokens`, basic auth, `sha1` in the response) —
  SADR-0009.
- OAuth2 app registration (`POST /user/applications/oauth2`, `confidential_client: true`) —
  SADR-0004, including its `127.0.0.1`-not-`localhost` lesson (IPv6 resolution bug) carried forward
  into the redirect URI.
- Woodpecker's health endpoint — **not previously checked in this project** — read directly from
  Woodpecker's own source this session (`server/router/router.go`:
  `base.GET("/healthz", api.Health)`; `server/api/z.go`: pings its DB, returns `204` on success,
  `500` with a body on failure) rather than assumed from documentation, which doesn't surface it
  clearly.

What is explicitly **not** proven: the playbooks as an integrated whole. `ansible-playbook site.yml`
has not run, cannot run on this host, and every playbook's header says so plainly. The OAuth-listing
idempotency check in `30-oauth.yml` is flagged separately as inferred (standard Gitea list-endpoint
shape) rather than independently re-confirmed against that specific endpoint.

## Decision

1. **Terraform's docker-provider mechanism is proven and can be trusted** for the scope tested:
   creating/destroying the network and volumes a profile runs on top of, idempotently, on a real
   Docker daemon, on Windows. Extend `terraform/local/` (more labels, driver options) freely.
2. **Do not write cloud-provider Terraform (`terraform/hetzner/`, `terraform/aws/`, ...) until there
   is a real account and credentials to test it against.** This project's whole discipline is
   live-testing before trusting; an untested cloud module is worse than an honestly-absent one.
3. **Do not cite "Checkov passes" as meaningful for the `docker` provider's resource types.** Update
   any future D6 dogfooding report to say explicitly what Checkov actually covers here (nothing, for
   `docker_network`/`docker_volume`) versus what it will cover once cloud or Kubernetes IaC exists.
4. **Ansible needs a real POSIX control node before anything here is trusted un-reviewed.** Options,
   in order of preference: (a) a real WSL2 distro installed specifically for this purpose — not
   attempted in this session, flagged to the user rather than done unilaterally, since installing a
   new WSL distro is a real system change beyond "write some playbooks"; (b) a Linux CI runner
   (Woodpecker itself, once `ansible/` needs to run unattended, is a candidate — circular but
   workable: the platform's own CI can validate the platform's own provisioning); (c) any macOS/Linux
   box available ad hoc. Once one exists: run `ansible-playbook -i inventory/localhost.ini
   site.yml` against a fresh `compose/minimal` checkout, fix whatever breaks, and update this SADR
   with the result before treating the playbooks as more than a well-grounded draft.
5. **Wiring `terraform/local/`'s resources into `docker-compose.yml` as `external: true`** is a
   real next step but deliberately deferred — it touches the one compose file every other SADR in
   this project has already verified end-to-end, and changing it needs the same full
   re-verification pass, not a drive-by edit bundled into unrelated IaC work.
6. `kubernetes/` remains untouched — correctly so. DESIGN.md's own roadmap places Kyverno/Falco/Trivy
   Operator at Milestone 6, after a K8s target exists; building it now would repeat exactly the
   "untested speculative IaC" mistake this SADR worked to avoid for the cloud-Terraform case.

## Reproduce it yourself

```
# Terraform (live-testable anywhere with Docker + Terraform installed):
cd terraform/local
terraform init
terraform plan
terraform apply -auto-approve
docker network ls --filter name=ssdlc-minimal
docker volume ls --filter name=ssdlc-minimal
terraform destroy -auto-approve

# Checkov coverage check:
checkov -d terraform/local --framework terraform -o json
# -> resource_count: 0 -- confirms the coverage gap, not a bug in this config

# Ansible (needs a real POSIX control node -- will fail with WinError 87 on
# native Windows, confirmed above):
cd ansible
ansible-playbook -i inventory/localhost.ini site.yml
```
