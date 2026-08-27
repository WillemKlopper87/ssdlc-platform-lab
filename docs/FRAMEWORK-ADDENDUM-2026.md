# Framework addendum 2026 — learning-first, trusted SSDLC automation

**Status:** Proposed enhancement to the SSDLC Framework v1.1.

**Purpose:** Preserve the original framework and workflow diagrams as the
governing baseline while recording current standards, supply-chain lessons, and
an approach suitable for newer developers. This addendum does not claim that
implementing its automation is ISO/IEC 27001 certification.

## What remains valid

The framework's lifecycle remains correct: secure design, developer guidance,
automated CI checks, deployment controls, runtime monitoring, and evidence
management reinforce each other. The current lab's SADRs, threat-driven
experiments, and staged rollout are therefore retained rather than replaced.

NIST SP 800-218 SSDF v1.1 remains a useful shared vocabulary for integrating
secure development practices into any SDLC. ISO/IEC 27001 remains the
governance target: a risk-managed ISMS requires people, process, evidence, and
review in addition to technical controls.

## 2026 updates to adopt

### 1. Version the awareness and verification standards

- Adopt **OWASP Top 10:2025** for developer awareness. Its explicit Software
  Supply Chain Failures and Mishandling of Exceptional Conditions categories
  make the platform's gate integrity, dependency, provenance, and resilience
  work visible as developer concerns, not only platform concerns.
- Pin **OWASP ASVS 5.0.0** as the application-verification reference. Use its
  versioned requirement identifiers in a risk-tiered catalogue; do not attempt
  to claim that scanners alone cover ASVS or the OWASP Top 10.
- Use the OWASP Top 10 for entry-level awareness and ASVS for verifiable
  requirements, design review, code review, and testing. A scanner result is
  evidence for a control, never proof that a product is secure by design.

### 2. Make trusted gate execution a mandatory control

The application code under review must never be able to define the policy,
pipeline, or approval logic that authorizes its own merge.

The platform's Gate Contract must record, at minimum:

- exact source commit SHA and target branch;
- platform gate version, trusted policy digest, and expected scanner set;
- runner identity and execution time;
- normalized findings and final decision;
- required bot and human approval outcomes.

The bot may approve only an attestation for the current PR head that satisfies
that contract. A successful status name or a count of approvals is insufficient
evidence by itself.

### 3. Treat supply-chain records as release inputs

The later release workflow should produce an SBOM, build provenance, and
signatures for the artifact and its security records. Cosign, Syft, signed OPA
bundles, and in-toto/SLSA-style attestations are complementary: they establish
what policy and source were used, what was built, and what may be deployed.

### 4. Teach before broadly blocking

For teams with newer developers, every finding must say:

1. what happened;
2. why it matters in plain language;
3. the safest next action; and
4. where to request a documented exception.

Initial enforcement is deliberately progressive:

- local pre-commit checks coach but do not form the security boundary;
- CI blocks verified secrets immediately;
- CI warns on inherited debt while a reviewed baseline is created;
- CI blocks new Critical findings; High findings block only after false-positive
  and remediation data demonstrate that the rule is useful.

Recurring findings should improve templates, examples, developer guidance, and
training rather than merely increasing the number of tools.

### 5. Add an AI-enabled product profile when needed

The existing AI-assistant policy remains applicable to developers using coding
assistants. Applications that themselves use LLMs need additional review:
prompt/data classification, model and provider approval, least-privilege tool
access, validation of model output before privileged actions, prompt-injection
tests, and incident handling. Use OWASP LLMSVS as an additional requirement
catalogue; it supplements ASVS and threat modeling, not replaces them.

### 6. Review maturity annually, not tool count

Use OWASP SAMM as an annual maturity discussion tool. It should help choose one
or two improvements that fit current capability; it is not a target to complete
all at once. Measure useful outcomes: false-positive rate, time to remediate,
aging exceptions, dependency-update age, escaped findings, and successful
restore/game-day results.

## Deferred capabilities

DefectDojo, Dependency-Track, OpenBao, dedicated runners, Kubernetes admission,
Falco/WAF/SIEM, a custom developer portal, and broad multi-repository rollout
remain deliberately deferred. They are valuable only after the trusted pilot
gate, beginner-friendly feedback, and evidence flow are proven in a real pilot.

## References

- OWASP Top 10:2025 — https://owasp.org/Top10/
- OWASP ASVS 5.0.0 — https://owasp.org/www-project-application-security-verification-standard/
- OWASP LLMSVS — https://owasp.org/www-project-llm-verification-standard/LLMSVS-v2.0-en.html
- OWASP SAMM — https://owasp.org/www-project-samm/
- NIST SP 800-218 SSDF v1.1 — https://csrc.nist.gov/pubs/sp/800/218/final
- SLSA provenance — https://slsa.dev/spec/v1.2/provenance
- CISA Secure by Design guidance — https://www.cisa.gov/news-events/alerts/2025/01/17/cisa-and-fbi-release-updated-guidance-product-security-bad-practices
