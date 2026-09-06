# Positioning

**Last reviewed:** 2026-09-05 · **Re-read cadence:** quarterly, or when a surveyed project ships the gap

## The five-step chain

1. **What would they do if we didn't exist?**
   Hand-sync markdown files across machines, re-explain the project each session, and run a
   per-machine memory store that strands what it learns. In practice: *a shell script and inertia.*
2. **What category does that put us in?**
   Not "AI memory" (crowded, per-machine, solved-ish). The category is **a control plane for
   agent context** — the layer between many harnesses and one authoritative store.
3. **What can we do that the alternatives structurally cannot?**
   A normalized scope chain (`global → org → team → project → repo → branch → task → session`)
   with *deterministic* instruction resolution, on the same store as memory, provenance,
   verification state, and skill state. Per-machine stores cannot resolve org-wide rules;
   file-sync tools cannot reason about trust or staleness.
4. **Who cares most?**
   Someone running several harnesses across several machines who has already been burned by a
   confidently-stale agent. Then: small teams who need shared rules without shared secrets.
5. **One-liner**
   > **Substrate is a self-hosted control plane that makes AI coding agents disposable by making
   > their context durable — one source of truth for instructions, memory, and skills, served to
   > any harness on any machine.**

## What is genuinely borrowed

Three of the four layers are solved or maturing and we take them as-is:

- **Skill format** — Agent Skills / `SKILL.md` (agentskills.io). We store state, not a new format.
- **Task orchestration pattern** — board-as-control-plane (Symphony), leases with `FOR UPDATE
  SKIP LOCKED` (NEEDLE), typed handoffs (Project Supervisor).
- **Session migration mechanics** — Claude Code transcripts under a matching cwd slug, resumable
  and version-gated. Community-proven, undocumented, therefore best-effort.

**The gap we build:** no surveyed project combines, on one store, the scope chain + deterministic
instruction resolution + provenance and verification + trust-weighted conflict handling +
symbol-linked staleness + budgeted, *explainable* context compilation + a permissioned review
queue + adapters that treat local files as caches.

## Licence and the free/paid line

**Apache-2.0, whole repo, no paid tier planned.**

The deciding question is whether strangers are meant to write against an interface here. They
are — the adapter contract, the MCP tool surface, and the rendered-file targets are all things a
third harness would implement. Nobody writes an adapter against a contract they cannot read, so
the interface must be permissively licensed; and with no commercial plan there is nothing for a
split-licence arrangement to protect. Apache-2.0 over MIT specifically for the explicit patent
grant, which matters for a project defining a wire format others implement.

Revisit trigger: if a hosted multi-tenant offering is ever seriously considered. That is a
non-goal today (`non-goals.md`), and reversing this after contributors arrive is the most
expensive reversible-in-theory decision an OSS project makes.
