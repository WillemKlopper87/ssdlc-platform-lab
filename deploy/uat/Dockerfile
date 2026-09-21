# UAT images. Build context is the repository root:
#   docker build -f deploy/uat/Dockerfile --target ops    -t ssdlc-uat-ops .
#   docker build -f deploy/uat/Dockerfile --target portal -t ssdlc-uat-portal .
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY portal/ ./
RUN CGO_ENABLED=0 go build -o /out/portal .

# "ops" runs the platform's own shell/python tooling (onboard-repo.sh,
# bot-approver.py, bootstrap.py) with the same dependencies a Linux admin
# host would have: git, curl, python3, and a docker CLI (generate-baseline.py
# shells out to `docker run` for Semgrep and Trivy).
FROM python:3.12-slim AS ops
RUN apt-get update \
    && apt-get install -y --no-install-recommends git curl ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir requests==2.32.3
COPY --from=docker:27-cli /usr/local/bin/docker /usr/local/bin/docker
ENV PYTHONDONTWRITEBYTECODE=1

FROM ops AS portal
COPY --from=build /out/portal /usr/local/bin/portal
CMD ["portal"]
