# Build from the repository root:
#   docker build -f gate-bundle/Dockerfile -t ssdlc-gate:pilot-2026-08-26 .
#
# The runner invokes this image by an operator-owned absolute command. Nothing
# in an application repository selects these tools, their versions, or policy.
FROM zricethezav/gitleaks:v8.30.1 AS gitleaks
FROM aquasec/trivy:0.74.0 AS trivy
FROM aquasec/trivy:0.74.0 AS trivy-db
RUN trivy image --download-db-only
FROM openpolicyagent/conftest:v0.69.0 AS conftest
FROM semgrep/semgrep:1.174.0

USER root
COPY --from=gitleaks /usr/bin/gitleaks /usr/local/bin/gitleaks
COPY --from=trivy /usr/local/bin/trivy /usr/local/bin/trivy
COPY --from=trivy-db /root/.cache/trivy /opt/ssdlc/trivy-cache
COPY --from=conftest /conftest /usr/local/bin/conftest

WORKDIR /opt/ssdlc
COPY normalise/ ./normalise/
COPY policy/ ./policy/
COPY policy-eval/evaluate-findings.py ./policy-eval/evaluate-findings.py
# docs/adr/0022: run-pilot-bundle.py imports gate_contract.attestation to
# compute its own policy_digest -- found live (a rebuild that a mocked unit
# test could not have caught, since that test imports the module from the
# repo root, not from inside the built image) that this was never copied in.
COPY gate_contract/ ./gate_contract/
COPY gate-bundle/run-pilot-bundle.py ./gate-bundle/run-pilot-bundle.py

RUN chmod -R a-w /opt/ssdlc/trivy-cache && chmod 0555 gate-bundle/run-pilot-bundle.py \
    && chown -R semgrep:semgrep /opt/ssdlc
# Found live (docs/adr/0025 verification, Trivy config DS-0002 + Semgrep
# last-user-is-root, both independently flagging this): the image never left
# USER root, so a container escape from any bundled scanner would already
# hold root inside a container this platform explicitly runs against
# untrusted PR content (SADR-0017). run-container.sh's own
# --cap-drop ALL --network none --read-only already bound the blast radius
# at the orchestration layer; this is the same property declared in the
# image itself, so it still holds if the bundle is ever invoked without that
# wrapper. "semgrep" (UID 1000) already exists in the base image -- reused,
# not invented, and chowned above so it can actually read/execute what was
# copied in as root.
USER semgrep
ENTRYPOINT ["python3", "/opt/ssdlc/gate-bundle/run-pilot-bundle.py"]
