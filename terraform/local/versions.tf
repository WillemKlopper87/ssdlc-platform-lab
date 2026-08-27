# terraform/local — the docker-level slice of the IaC layer named in
# DESIGN.md's repository structure ("terraform/ hosts, networks, volumes,
# K8s"). Scope is deliberate and narrow: this manages the Docker network
# and named volumes a deployment profile runs on top of, on whatever host
# already has a Docker daemon — the same slice DESIGN.md's `minimal`
# profile is "deliberately just the CI loop" for Milestone 1 (see
# compose/minimal/docker-compose.yml's own header comment).
#
# What this does NOT do, on purpose, and why: "hosts" in DESIGN.md's
# terraform/ description means provisioning the actual VM/cloud host a
# profile deploys onto (Hetzner/AWS/DO per DESIGN.md's Deployment
# profiles table). That needs a real cloud provider and credentials this
# environment does not have, and this project's own discipline (every
# SADR live-tests before trusting a mechanism) means untested cloud HCL
# does not belong here yet. Add a provider-specific host module
# (terraform/hetzner/, terraform/aws/, ...) once there is a real target to
# test it against — do not write one speculatively.
#
# Provider pinned by exact version per docs/PINNED_VERSIONS.md, checked
# against the registry's actual latest release on 2026-08-24, same
# discipline as every other pin in this project.

terraform {
  required_version = ">= 1.15.0"

  required_providers {
    docker = {
      source  = "kreuzwerker/docker"
      version = "4.5.0"
    }
  }
}

provider "docker" {
  host = var.docker_host
}
