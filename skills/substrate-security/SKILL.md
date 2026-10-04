---
name: substrate-security
description: Use when planning, implementing, or reviewing Substrate identity, artifact authorization, MCP/admin interfaces, transfer, recovery, telemetry, releases, security tests, or enterprise security claims.
---

# Substrate security decisions

Apply this workflow only to the Substrate repository. Resolve its current checkout and read
`AGENTS.md`, `SECURITY.md`, `docs/plans/security.md`, and the affected contract before acting.
Use repository documents as the source of truth; re-read them instead of treating memory or
this skill as implementation evidence. In a worktree, use that worktree's documents.

## Route the work

| Change | Read with the security roadmap |
|---|---|
| Artifact/session/export policy | `docs/architecture/artifacts.md`, `sharing.md`, and `repositories.md` |
| Identity, hub, transfer, keys, recovery | `docs/architecture/security.md`, `deployment.md`, and `onboarding.md` |
| Offline access, deletion, restore | `docs/architecture/offline.md` and `reconciliation.md` |
| MCP, browser, adapters | `docs/architecture/delivery.md` and `docs/product/first-release.md` |
| CI, distribution, assurance | `docs/development.md`, `SECURITY.md`, and `docs/plans/security.md` |

Architecture filenames in this table are relative to `docs/architecture/`. Pin standard,
protocol, SDK, and client versions when implementing; verify the applicable publisher guidance.
Protocol documentation alone does not prove harness compatibility.

## Turn a request into a bounded change

1. Identify the implemented surface, caller/input, protected resource, governing authority,
   and proposed claim. Bootstrap provides loopback status/static routes; artifact storage,
   runtime, MCP, authentication, enrollment, and synchronization remain unimplemented until
   their acceptance evidence exists. Source and tests alone do not prove a passing control.
2. Classify relevant decisions as confirmed, proposed, or open. Preserve one-node first release,
   initial HTTPS/single coordinator, finite shared offline grants, and independent wholly owned
   Personal operation. Future peer-to-peer transfer does not select a new initial topology.
3. For transfer/recovery, resolve roadmap D10–D12 first: custody/effective decryptors, recoverable
   scope, and offline enforcement. Enterprise assigned-scope recovery is required; customer
   custody is a recommendation, not a selection. Do not invent durations, keys, or an absolute
   nobody-else-can-decrypt promise. Record any selected/changed decision in the contract.
4. Keep content, management, enrollment, recovery, and analytics authority separate. Consider
   common host/IAM/deployment and hub-served browser code. Valid tokens/certificates, imported
   instructions, and administrator UI roles cannot bypass object/action/scope checks. Imported
   executable skills need explicit trusted promotion; relay does not authorize execution.
5. Approve telemetry fields/purpose/recipient/permissions/retention/region before adding collection.
   Keep content, credentials, raw tool arguments, and repository paths out of routine telemetry.
   Preserve publication restrictions when producing documentation or evidence.
6. Select the smallest proving negative/failure checks from the roadmap. Before customer/internal
   network transfer, require agreed design and hostile-peer/hub, replay/rollback, scoped recovery,
   offline removal, deletion/restore, and crypto-boundary evidence. Never claim remote erasure of
   copied plaintext or extracted keys. Do not prune the only test protecting an invariant.
7. Update paired prose and the control evidence record with status, owner, revision/environment,
   result, limits, and review trigger. Run checks appropriate to the changed surface; for docs,
   build pages and check links. Report exact commands and material untested boundaries. A
   provenance/signature or framework mapping is not a SLSA/ASVS/audit achievement.

Unresolved choices remain explicit design work. This skill grants no authority to publish,
purchase audits, change external services, or expand the user's requested scope.
