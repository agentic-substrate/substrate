# Security policy

## Reporting a vulnerability

Report privately through GitHub's **[Private vulnerability
reporting](https://github.com/agentic-substrate/substrate/security/advisories/new)** — the Security tab
of this repository. Please do not open a public issue for a suspected vulnerability.

This is a personal project without a staffed on-call rotation. Expect an acknowledgement within
7 days and an assessment within 30. If a fix is warranted you will be credited in the advisory
unless you ask otherwise.

Substrate is pre-alpha and self-hosted behind Tailscale by design. There is no hosted service,
so there is no production environment to compromise other than an operator's own.

## What is *not* a vulnerability

These are documented design positions. Reports on them will be closed with a pointer here.

- **Prompt injection through stored memory.** This is treated as a **layered mitigation, not a
  boundary** (EDD §13). Agent observations enter as `unverified` and are excluded from default
  packs; only corroboration or human action promotes them; instructions live in a separate
  deterministic table so no agent path turns a memory into a directive; and memory renders as
  quoted data with a preamble. A demonstration that the *rendering fence* can be crossed is
  expected — it is explicitly the weakest layer. A demonstration that a memory can reach the
  **instruction** table, or be promoted without human or corroborating action, **is** a
  vulnerability and we want to hear about it.
- **A single node with no HA.** Homelab by design. Adapters survive a 24-hour outage; that is
  the availability story, not a gap.
- **No public exposure hardening.** The service is reachable only over Tailscale. Findings that
  require exposing it to the public internet describe an unsupported deployment.
- **Anything in an example, fixture, or `docs/design/`.** Those are records and sample data.
- **Dependency CVEs with no reachable call path.** `govulncheck` runs in CI precisely because
  reachability is the thing that matters. Show the path.

## What we especially want

- Cross-team reads that RLS should have stopped — RLS is a backstop the design leans on hard.
- A path by which an `agent_*` principal writes above its minting user's permissions, or sets a
  status it may not set.
- Token handling flaws: tokens are hashed at rest, per (principal, machine), revocable, and
  24-hour TTL for agent principals.
- A memory containing a secret that the write-path scanner lets through.
- Anything that lets an unapproved skill version become the active one.

## Released tags are immutable

A published release tag is never moved, deleted, or re-pointed at different code, and a
repository ruleset enforces this. If a release is bad, it is superseded by a new tag and the old
one is marked broken in its release notes. If you observe a tag that no longer matches the code
it was published from, treat that as a security report.
