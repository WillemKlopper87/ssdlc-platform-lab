# SADR-0014: `compose/minimal` now runs on Terraform-provisioned network and volumes — full stack re-verified

**Status:** Confirmed by experiment, full stack up, a real pipeline run, and a persistence proof
**Date:** 2026-08-26
**Milestone:** 1/3 — closes SADR-0010's own deferred item ("not yet wired into
`compose/minimal/docker-compose.yml`... tracked below, not done here")
**Security & Privacy Impact:** Low directly (no change to the gate or trust path — same containers,
same images, same environment variables; only *which process created the network and volumes*
changes). Indirectly relevant to D6: this is what actually makes the platform's own infrastructure
reviewable as code, rather than an implicit side effect of `docker compose up`.

## Question

SADR-0010 built and proved `terraform/local/` — a real network and three real volumes, full
apply/destroy lifecycle — but deliberately stopped short of wiring `compose/minimal` to use them,
flagged as needing "a full re-verification pass of the already-tested minimal stack" rather than a
drive-by edit. This does that pass: does the full stack (Postgres, Gitea, Woodpecker server + agent)
actually come up correctly against externally-provisioned resources, and does everything this project
has already proven about this profile — step containers reaching Gitea, OAuth, a real pipeline run —
still hold once compose no longer owns the network and volumes it used to create implicitly?

## Method

Changed three things in `compose/minimal/docker-compose.yml`:

1. The top-level `networks: default:` entry now reads `external: true, name: ssdlc-minimal` —
   overriding the implicit default network every service already attached to (none of them declared
   `networks:` explicitly), rather than adding that key to each service individually. One place to
   read; one place that can drift from `terraform/local/main.tf`.
2. All three named volumes (`postgres-data`, `gitea-data`, `woodpecker-server-data`) marked
   `external: true` with `name:` matching Terraform's exact resource names.
3. `WOODPECKER_BACKEND_DOCKER_NETWORK` updated from the old auto-generated `minimal_default` to
   `ssdlc-minimal` — this is the setting that determines which network the agent attaches *pipeline
   step* containers to, independent of what compose itself does at service-startup time, and it would
   have silently kept working against a network that no longer matched reality if left unchanged.

Ran `terraform apply` first (compose refuses to start against `external: true` resources that don't
exist yet — this is deliberate friction, not a rough edge: it makes "someone forgot to provision
infrastructure" a hard stop instead of compose quietly creating its own).

## Result

Brought the full stack up service by service, verifying at each step rather than assuming compose's
`external: true` handling worked from the config alone:

```
docker compose up -d postgres
  -> no "Volume ... Creating" line at all (confirms it recognized the external volume,
     didn't try to create its own)
  docker inspect ssdlc-minimal-postgres --format '{{json .NetworkSettings.Networks}}'
  -> attached to "ssdlc-minimal", the Terraform network, not an auto-created one

docker compose up -d gitea / woodpecker-server / woodpecker-agent
  -> all healthy, OAuth app registered and login flow completed exactly as SADR-0004 proved
```

**The real proof — a pipeline step container reaching Gitea over the renamed network:**

```yaml
steps:
  prove-network:
    image: alpine:3
    commands:
      - apk add --no-cache curl
      - curl -sf http://gitea:3500/api/healthz && echo "step container reached gitea over the Terraform-managed network"
```

```
clone -> success
prove-network -> success

log: {"status": "pass", ...} step container reached gitea over the Terraform-managed network
```

This is the one thing that could plausibly have broken silently: `WOODPECKER_BACKEND_DOCKER_NETWORK`
is read by the *agent*, not by compose, and a stale value would have made every pipeline's clone step
fail exactly the way SADR-0005 originally found and fixed — but pointed at a network that simply
doesn't exist under this name anymore, a different failure mode than that SADR's original bug. Updating
it was necessary, not optional, and this run proves it was also sufficient.

**Persistence, proven, not assumed**: stopped and removed the `gitea` container entirely
(`docker compose stop gitea && docker compose rm -f gitea`), brought it back up fresh, and confirmed
the `gateadmin` account created before the removal still existed and was still admin — the actual
point of a named volume, holding under the exact operation (container replacement) that would expose
a volume that wasn't really externally durable.

**Full lifecycle closed cleanly**: `docker compose down` (deliberately without `-v`) left the external
network and volumes untouched, confirmed via `docker network ls` / `docker volume ls` directly rather
than trusting compose's own exit message — then `terraform destroy` removed all four cleanly.

## Decision

1. **`compose/minimal` now depends on `terraform/local/` being applied first.** This is now
   documented directly in the compose file's own header comment, not just in this SADR — anyone
   running `docker compose up` cold, without reading further, gets a clear "external volume not
   found" error rather than silent divergent behavior.
2. **D6's dogfooding claim is now real for this slice**, with the same honest caveat SADR-0010
   already recorded: Checkov still has no policies for `docker_network`/`docker_volume` resource
   types, so the value here is reviewability and reproducibility, not a scan result.
3. **The `WOODPECKER_BACKEND_DOCKER_NETWORK` dependency on the network's exact name is a real,
   easy-to-miss coupling** worth remembering the next time this network is renamed or a second
   profile is added: it lives in an environment variable, not inferred from the compose file's own
   network block, and nothing would have caught a mismatch except the pipeline's clone step failing.
4. Nothing else in this profile changed — same images, same versions, same environment variables
   otherwise. Every prior SADR's proof about this profile's behavior (OAuth flow, CSRF handling,
   status contexts, approval mechanics) continues to hold unmodified.

## Reproduce it yourself

```
cd terraform/local
terraform init && terraform apply -auto-approve

cd ../../compose/minimal
cp .env.example .env   # POSTGRES_PASSWORD, WOODPECKER_AGENT_SECRET, WOODPECKER_GRPC_SECRET
docker compose up -d postgres
# wait healthy, then:
docker compose up -d gitea
# wait http://127.0.0.1:3500/api/healthz == "pass", create admin, mint a token,
# register an OAuth2 app (redirect_uri http://127.0.0.1:8000/authorize), put creds in .env
docker compose up -d woodpecker-server woodpecker-agent
# push a .woodpecker.yml step that curls http://gitea:3500/api/healthz -- confirms step
# containers reach Gitea over the Terraform-managed network, not just the host-facing services
docker compose down          # NOT -v -- network/volumes are externally managed now
cd ../../terraform/local
terraform destroy -auto-approve
```
