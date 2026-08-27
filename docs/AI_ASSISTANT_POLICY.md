# AI assistant policy

Implements framework §4 (mandatory internal-first LLM review) and the additions in
[`FRAMEWORK-ADDENDUM-2026.md`](FRAMEWORK-ADDENDUM-2026.md) §5. Scope: developers using an AI coding
assistant to write or review code destined for an onboarded repository. A separate, additional review
applies to any *product* that itself embeds an LLM — see "AI-enabled products" below; that is a
different and larger surface than "a developer used Copilot/Claude/etc. to write a function."

## The core rule

**Code proposed or written with AI assistance is reviewed exactly like any other code — the gate does
not know or care how a diff was produced, and no diff gets special trust for having a human or an AI
as its nominal author.** [`GATE_CONTRACT.md`](GATE_CONTRACT.md)'s invariants apply identically either
way: the same scanners run, the same policy blocks Critical/High findings, the same approval rules
apply. This is deliberate — an AI assistant is exactly as capable of introducing a vulnerable
dependency, a hardcoded credential, or an injection flaw as a human is, and the platform's whole
premise (DESIGN.md's "Goal") is that merge is blocked on *what the code does*, not on who or what
proposed it.

## Internal-first review (framework §4, not yet enforced by tooling)

The framework's intent is that code passes through an **internally-hosted** model
(`Continue` + `Ollama`, per DESIGN.md's component inventory) before it reaches any *external* AI
assistant — keeping proprietary code out of a third-party model's training/logging surface by default.

**Current status: designed, not built.** No Ollama host exists in this environment; see
[`DEVELOPER_SETUP.md`](DEVELOPER_SETUP.md) and `DESIGN.md`'s open question on model/host sizing. Until
it does, this control does not exist as a technical enforcement point — it is a process expectation
only, and this document should not be read as claiming otherwise. Do not present an unenforced control
as an enforced one in any ISO evidence export (see [`COMPLIANCE.md`](COMPLIANCE.md)).

## What every finding still owes a developer (framework addendum §4)

Regardless of whether code was AI-assisted, every gate finding must explain, per
`FRAMEWORK-ADDENDUM-2026.md`'s "teach before broadly blocking" principle:

1. what happened;
2. why it matters, in plain language;
3. the safest next action; and
4. where to request a documented exception.

This matters more, not less, for AI-assisted code: a developer who did not write a line by hand needs
the finding to teach the underlying issue, not just report a rule ID, or the same class of mistake
recurs the next time an assistant proposes similar code.

## Data handling

Do not paste production credentials, customer data, or details of a suspected security incident into
any AI assistant's context — external or internal. This is the same rule [SECURITY.md](../SECURITY.md)
states for pipeline logs and test repositories, applied to assistant conversations too: assistant
context, like a build log, should be treated as potentially retained by the provider.

## Approved external assistants

No approved-tool list exists yet in this repository. `FRAMEWORK-ADDENDUM-2026.md` §6 calls for
reviewing "the framework's own quarterly reviews (... §4 approved-AI-tool list ...)" — until that
review happens and a list is published here, treat any external assistant's output the same as code
from an unfamiliar contributor: reviewed in full, never merged on trust.

## AI-enabled products (a different, larger surface)

If the *product* being built in an onboarded repository itself calls an LLM — not "a developer used an
assistant to write code," but "the shipped application has a prompt path" — `FRAMEWORK-ADDENDUM-2026.md`
§5 requires additional review beyond this document's scope: prompt/data classification, model and
provider approval, least-privilege tool access for the model, validation of model output before any
privileged action, prompt-injection testing, and incident handling, using **OWASP LLMSVS** as an
additional requirement catalogue alongside ASVS and ordinary threat modeling — not a replacement for
either. Nothing in this platform currently automates that review; it is a manual gate applied when a
repository's stated purpose includes an LLM-calling feature.

## What this platform's gate does not verify

The gate verifies what the code *does* (secrets, SAST findings, vulnerable dependencies) and who
*approved* it. It does not and cannot verify how a given diff was produced, and no field in
[`GATE_CONTRACT.md`](GATE_CONTRACT.md)'s contract records assistant provenance. If that becomes a
requirement (for licensing, provenance, or audit reasons), it needs its own decision record, not an
assumption folded silently into this document.
