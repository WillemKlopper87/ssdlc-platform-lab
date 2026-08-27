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
COPY gate-bundle/run-pilot-bundle.py ./gate-bundle/run-pilot-bundle.py
COPY gate-bundle/semgrep-rules.yml ./gate-bundle/semgrep-rules.yml

RUN chmod -R a-w /opt/ssdlc/trivy-cache && chmod 0555 gate-bundle/run-pilot-bundle.py
ENTRYPOINT ["python3", "/opt/ssdlc/gate-bundle/run-pilot-bundle.py"]
