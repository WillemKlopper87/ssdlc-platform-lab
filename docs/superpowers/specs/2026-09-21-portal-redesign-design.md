# Portal redesign: an issue-centred, role-aware front door

**Status:** draft for review
**Supersedes the screen flows in:** [`2026-09-07-ssdlc-portal-design.md`](2026-09-07-ssdlc-portal-design.md) (its data rules and trust model still apply).
**Builds on:** [`2026-09-21-tiered-platform-design.md`](2026-09-21-tiered-platform-design.md) (tiers, reporting service, DefectDojo, Grafana) and the reporting service already implemented from [`../plans/2026-09-21-reporting-service.md`](../plans/2026-09-21-reporting-service.md).
**Visual reference:** [`2026-09-21-portal-redesign-mockup/`](2026-09-21-portal-redesign-mockup/): `prototype.html` is a clickable prototype (open it in a browser; the URL hash such as `#role=approver&view=issues` deep-links states) and the PNGs are renders of it. The prototype is a design artefact, not production code: its data is fake and it is a single-page script, which the implementation will not be (see Technical approach).

## Why

Testers found the current portal's flows wrong in four ways: there is **no overall picture** (the landing page lists only my PRs), **findings are buried** (visible only inside one PR's report), **exceptions are disconnected** (a separate form, no visible state, no clear queue), and **navigation and onboarding are confusing** (unclear where to start or what each role does).

The redesign combines two familiar products: **Snyk's** issue-first flow (a prioritised issue list, an issue page with why-and-how-to-fix, and an ignore flow) and **DefectDojo's** project and risk model (projects with grades, SLA clocks, status flags, bulk actions, risk acceptance with expiry).

## Decisions taken (with the user, during brainstorming)

1. **Navigation:** the hybrid option. Sidebar: Overview, Projects, Issues, Exceptions, Admin (admins only), Help.
2. **Issue page:** single page with the fix guidance on the left and a status/action panel on the right (Snyk-like), shown as a slide-in panel over the list and also reachable by URL.
3. **Overview:** headline numbers, then "Needs your attention", then charts, then the projects table. Adapts to the signed-in role.
4. **Findings data source:** live from open pull requests plus each repo's baseline, on every tier. DefectDojo adds history, closed findings and SLA clocks on the `full` tier. The portal keeps its rule of owning no security state.
5. **DefectDojo-inspired additions:** project letter grades, SLA countdowns, a status-flag row, and bulk actions.
6. **Gitea and Woodpecker:** deep links that open in a new tab, everywhere a project, PR, file or pipeline is shown, plus a Quick launch block in the sidebar.
7. **Help page** and contextual "Why?" links.
8. **AI suggestion** on the issue page: optional, off by default, later phase.

## Roles

Taken from Gitea, as today. **Developer**: any signed-in user. **Approver**: member of the `approvers` team. **Admin**: platform admin. The Overview, sidebar badge and available actions adapt; approvers can never approve their own requests.

## Screens

**Overview.** Greeting; four headline numbers (blocked PRs, open issues, exceptions pending, PR pass rate over 7 days), each linking to the matching filtered list; **Needs your attention**, ordered by urgency (approvals waiting, my blocked PRs, my exceptions about to expire, and for admins "Set up a project"), with a friendly empty state; open-issues trend and severity donut; projects table, worst first, with grade, gate, issue counts, health bar and links to Gitea and Woodpecker.

**Projects / Project page.** Cards or table with a letter grade and health ring. A project page has tabs: **Pull requests** (gate state, author, links to the PR and its pipeline), **Issues** (the global list pre-filtered), **Baseline** (existing debt and its burn-down; it only shrinks), **Settings** (the gate rules in force and who can approve). Header buttons open the repo in Gitea and Woodpecker.

**Issues.** One list across projects: severity and status filter chips, an "include baseline" toggle, search, sortable columns including **Fix within (SLA)**, and checkboxes. Selecting rows shows a bulk bar: request exceptions (with one shared reason), and for approvers approve or decline; each button shows how many of the selection it applies to. Shortcuts: `/` filters, `Ctrl K` jumps anywhere.

**Issue page (panel).** Title and severity; a **status-flag row** (Active, Verified, False positive, Duplicate, Risk accepted, Baseline, Overdue); the code with the offending line highlighted; links to the file, PR and pipeline in Gitea and Woodpecker; **why it matters**; **how to fix** with a copyable suggested change. Right column: status, **SLA progress**, **request an exception** (reason, expiry 7 to 90 days) or, for approvers, **approve/decline**, "mark as false positive", and a **history** timeline. Requesting an exception happens here; the issue's status shows everywhere.

**Exceptions.** Tabs Waiting for me / My requests / All. Each card shows the requester's reason, expiry and approve/decline (replaced by "Waiting for another approver" for your own request), and links to the issue. The sidebar badge shows the approver's pending count. **Approving an exception re-runs the pull request's pipeline** (the reporting service calls Woodpecker's restart, as DESIGN.md D5 describes) so the PR reads the approved record and turns green without the developer pushing anything; declining does not re-run. The issue page and PR rows also offer a **Re-run gate** button (for a flaky scan), available to the PR's author and approvers.

**Admin** (admins only). **Set up a project**: pick a repository from a list, review what will be added (pipeline, baseline scan, branch protection, bot access), run with live progress, end with a link to the new project. **Platform health** from the reporting service (Gitea, Woodpecker, agent, reporting service, address redirect, and the AI assistant when present), the detected server tier, and Posture and logs on the tiers that include them.

**Help.** Role-aware "first steps", a searchable list of common questions (why was my PR blocked, how to fix, when to request an exception, who can approve, grades, SLA, baseline, false positives, signing in to Gitea/Woodpecker, the AI suggestion, shortcuts), each answer offering a button to the relevant screen. A "Why?" link on a blocked PR opens the right answer directly.

## Gitea and Woodpecker: links now, control later

Gitea is the identity provider for the portal and for Woodpecker. A user signed in to Gitea in their browser moves to Woodpecker without another login (Woodpecker's OAuth completes silently, after a one-time "Authorize" click). The portal cannot pass its own token to those sites (browsers keep separate sessions per address); its token is used **server-side** to call their APIs on the user's behalf.

Phased intent, since the user wants the portal to become the main point of access:
1. **This redesign:** deep links to repo, PR, file and line, and pipeline run.
2. **Later:** safe actions from the portal using the user's own permissions (re-run a failed pipeline, view pipeline logs, leave a review).
3. **Later still:** broader control (create/merge PRs), only where the platform's two-person and gate rules can be enforced from the portal.

Constraint to plan for: Woodpecker actions need a **per-user Woodpecker token**. Today the portal uses one admin token for Woodpecker, so per-user control needs a per-user sign-in step first; otherwise everyone would act as admin. Phase 1 is unaffected.

## Optional AI suggestion (later phase, off by default)

A section on the issue page, **✦ AI suggestion (beta)**: a button "Suggest a rewrite", a short "thinking" state, then an explanation and a before/after diff with "Copy rewrite" and thumbs feedback, labelled "AI-generated, verify before use".

**Flow once a pull request is open.** The pipeline runs, the gate decides, and within about 60 seconds the reporting service posts or edits its one deterministic sticky comment (already built; not AI). The developer opens an issue in the portal and clicks **Suggest a rewrite**; the reporting service sends a redacted snippet to the local model and shows the result in seconds. Nothing is stored by the portal.

**Sharing it: "Post to PR" (chosen).** After reading a suggestion the developer can click **Post to PR**. The reporting service then adds a **separate comment** on the pull request, marked with its own hidden marker (distinct from the sticky gate comment's marker), labelled "AI-generated", "requested by <user>", with the redacted-and-safe rewrite in a code block. The comment lives in Gitea, the system of record; the issue's history records who posted it. Gitea has no GitHub-style one-click "apply suggestion", so the developer copies or applies it by hand. Only available for issues on a pull request the user can see, never for baseline issues; one AI comment per issue, edited in place if posted again.

**Not chosen: automatic posting on every pull request.** It would attach unverified model output to every PR, cost model time on every push, and encourage blind acceptance on blocked PRs. It may be offered later as a **per-project setting, off by default**.

**Posting does not re-run the gate.** A comment does not change the code, and the pipeline re-runs only on a new push (a fix), on an approved exception, or on an explicit Re-run gate. AI comments never trigger or influence a run.

Additional rules for posting: the gate and the bot's approval logic ignore AI comments entirely; posted text passes through the same redaction as the prompt (a pushed comment can never contain the secret value); posting is rate-limited per user.

Rules:
- **Advisory only, never in the merge decision.** The portal is identical without it.
- **Local model on the operator's own host.** A small code model (for example a 7B-class model served by Ollama), pointed at by address, so it can live on a larger machine. Code never leaves the platform.
- **Redact before prompting.** The finding's secret value and any token-shaped strings are removed from the snippet before the prompt is built; the response panel says how many values were hidden. A leaked key is the most common Critical finding, so this is mandatory, tested, and fails closed (if redaction cannot run, no request is sent).
- **Feature flag.** Enabled only when configured; the portal shows "AI suggestions are not enabled on this server" otherwise, and Admin health lists the assistant's state.
- **Not a tier requirement.** It fits the memory-tier scheme as an opt-in extra (about 6 to 8 GB for the model), not part of `core`, `standard` or `full`.
- Ollama and Continue were excluded from the current UAT deployment for hardware reasons; this feature therefore needs its own decision on where the model runs before it is built.

## Data the screens need

The reporting service today builds a report for one PR. The redesign needs aggregated endpoints, all read-only:

| Need | Source | Tier |
|---|---|---|
| Issues across projects (open PR findings) | `report.Build` over every open PR of every active repo (already used by the poller) | all |
| Baseline issues and counts | each repo's `.ssdlc/baseline.json` via Gitea's contents API | all |
| Baseline burn-down history | git history of that same file (one commit per shrink) | all |
| Exception state on an issue | records in the exceptions repo, joined to findings by fingerprint | all |
| PR pass rate (7 days), pipeline duration | Woodpecker pipeline list | all |
| Open-issues trend, gate outcomes over time | Prometheus, scraped from the reporting service | `standard`+ (hidden on `core`) |
| Findings history, closed findings, SLA clocks for non-PR findings | DefectDojo | `full` |
| Platform health, tier | reporting service `/api/v1/health` | all |

The SLA clock on `core` starts when the pull request that introduced the issue was opened; issues on the default branch or in the baseline have no clock. On `full`, DefectDojo's first-seen date is used.

**Grade:** score = 100 − 35×critical − 18×high − 6×medium − 2×low over open issues; A ≥ 90, B ≥ 75, C ≥ 60, D ≥ 40, else F. **SLA targets (prototype values):** Critical 2 days, High 7, Medium 30, Low 90. Both are configuration and must be confirmed against framework §2.4 (see Open questions).

## Technical approach

- Keep the original portal decision (D10): **server-rendered Go templates with small vanilla-JS or HTMX-style enhancements, one binary, no SPA build chain**. The prototype's slide-in panel, command palette (`Ctrl K`), filters and bulk bar become small progressive-enhancement scripts over server-rendered pages, each page addressable by URL.
- All assets served by the portal itself: **vendor the fonts and any script** (no Google Fonts or CDN calls, since the platform runs on isolated networks).
- New portal pages read the reporting service's aggregated endpoints (service token), except where a user's own permissions must apply, which use the user's Gitea token as today.
- Feature visibility follows `PORTAL_FEATURES` from the tier design, plus `ai`. Sidebar entries stay visible when a feature is missing, and the page says why ("not available on this server").
- Keep the current dark token palette (`portal/web/static/tokens.css`); the prototype uses the same OKLCH values.

## Delivery order

Each step gets its own plan and is verified before the next.

1. **Shell:** new layout, role-aware sidebar with badges, Help, Ctrl-K palette, Gitea/Woodpecker deep links and Quick launch. (Reuses today's data.)
2. **Aggregated data:** reporting-service endpoints for projects, issues (PR findings plus baseline), overview numbers and grades.
3. **Overview, Projects, Project page, Issues list** with filters, SLA and bulk actions.
4. **Issue page** with status flags, guidance and history; then the **exception flow** on it and the **Exceptions** queue.
5. **Admin:** project setup flow and Platform health.
6. **Later, optional:** AI suggestion; per-user Woodpecker sign-in and portal-side pipeline actions.

## Prerequisite and known dependencies

- The exception workflow has open defects recorded in the UAT readiness report (the second approver is refused, approved exceptions are not consulted by the pipeline, the exceptions repo is created under the wrong owner). **Step 4's exception screens are not meaningful until those are fixed**; that fix is a separate piece of work and should be scheduled before or with step 4.
- The portal PR page does not yet display report notes (for example "gate status unavailable"); the new issue and project pages must show them visibly.

## Non-goals

Replacing Gitea's or Woodpecker's own UIs; multi-tenant organisations; a light theme and a full mobile layout (the prototype is desktop-first and collapses to narrower widths only); editing rules or policy from the portal; controlling Gitea or Woodpecker beyond deep links in this redesign.

## Open questions

- **SLA targets and grade thresholds:** confirm against framework §2.4 and the org's risk appetite before freezing.
- **Trend charts on `core`:** hidden (proposed) or approximated from Woodpecker's pipeline history.
- **Per-user Woodpecker tokens:** how each user obtains one (an OAuth step in the portal) before any control features.
- **AI model and host:** which model, and where it runs.
- **Bulk exception requests:** one shared reason for all selected issues (as prototyped) versus a reason per issue.
- **Baseline visibility:** shown by default in the Issues list (prototyped) or hidden by default.
