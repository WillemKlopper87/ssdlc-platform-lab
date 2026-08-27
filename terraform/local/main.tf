# Manages the network and named volumes that compose/minimal/docker-compose.yml
# currently creates implicitly (an auto-named default network, three bare
# `volumes:` entries with no labels, no documented ownership). Terraform
# owning these explicitly is what makes them reviewable, diffable, and
# Checkov-scannable -- the actual point of D6's "the platform's own
# Compose/IaC passes Checkov" dogfooding rule, which an implicit
# compose-managed network/volume set cannot satisfy since there is no
# declarative file for a scanner to read.
#
# compose/minimal/docker-compose.yml consumes these as `external: true`
# resources. Apply this directory before starting that profile; Compose will
# deliberately fail instead of silently creating unmanaged replacements.

resource "docker_network" "minimal" {
  name   = "ssdlc-minimal"
  driver = "bridge"

  # Step containers (Semgrep, Trivy, gitleaks, the approval-check step)
  # need egress to pull scanner images and, for sast, live Semgrep
  # registry rulesets (see fast.woodpecker.yml) -- internal=true would
  # break that today. Revisit once rulesets are vendored per DESIGN.md's
  # "Rule-set updates" section and an internal registry mirror exists.
  internal = false

  labels {
    label = "ssdlc.profile"
    value = var.profile
  }
  labels {
    label = "ssdlc.managed-by"
    value = "terraform"
  }
}

resource "docker_volume" "postgres_data" {
  name = "ssdlc-${var.profile}-postgres-data"

  labels {
    label = "ssdlc.profile"
    value = var.profile
  }
  labels {
    label = "ssdlc.component"
    value = "postgres"
  }
}

resource "docker_volume" "gitea_data" {
  name = "ssdlc-${var.profile}-gitea-data"

  labels {
    label = "ssdlc.profile"
    value = var.profile
  }
  labels {
    label = "ssdlc.component"
    value = "gitea"
  }
}

resource "docker_volume" "woodpecker_server_data" {
  name = "ssdlc-${var.profile}-woodpecker-server-data"

  labels {
    label = "ssdlc.profile"
    value = var.profile
  }
  labels {
    label = "ssdlc.component"
    value = "woodpecker-server"
  }
}
