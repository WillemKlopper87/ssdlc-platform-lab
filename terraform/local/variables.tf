variable "docker_host" {
  description = <<-EOT
    Docker daemon endpoint. No single default is correct across hosts --
    found live on this machine that Docker Desktop's actual named pipe is
    NOT the commonly-documented default the provider itself falls back to:

      confirmed via `docker context inspect` -> Endpoints.docker.Host:
        npipe:////./pipe/dockerDesktopLinuxEngine

    vs. the provider's own built-in default of npipe:////./pipe/docker_engine
    (which is correct for some Windows Docker installs, just not this one).
    Other common values:
      Linux / most CI runners:  unix:///var/run/docker.sock
      Docker Desktop (macOS):   unix:///$HOME/.docker/run/docker.sock

    Always check `docker context inspect` on the target host rather than
    assuming any of the above -- that is how this default was found to be
    wrong on the first attempt.
  EOT
  type        = string
  default     = "npipe:////./pipe/dockerDesktopLinuxEngine"
}

variable "profile" {
  description = "Deployment profile these resources belong to (compose/<profile>/). Used only for labels."
  type        = string
  default     = "minimal"
}
