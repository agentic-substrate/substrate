# Positioning

**Last reviewed:** 2026-09-08 · **Re-read cadence:** quarterly

## One sentence

**Substrate is a self-hosted control plane for durable agent context: instructions,
preferences, memories, and approved skills, available to any coding harness on any machine.**

Its long-term goal is for knowledge and work to carry forward across sessions, with provenance,
review, and portable checkpoints.

## Who it serves

Start with developers running several harnesses across several machines: people who repeat
project briefings, reconcile drifting rules, and maintain separate memory and skill stores.
The later multi-team horizon extends that model to teams that need shared rules and procedures
while keeping private context isolated.

## Why a context layer

| Existing approach | What Substrate adds |
|---|---|
| Copy or sync harness configuration | Authoritative scoped instructions, a separate preference overlay, and drift review |
| Store facts in each harness's memory | Shared memories with provenance, verification, visibility, and retained history |
| Copy skill directories | Distribution of approved Git versions according to scope and visibility |
| Keep working in the original conversation | Planned checkpoint-based continuity across harnesses and machines |

The design combines these responsibilities without treating all context alike. Instructions
are deterministic, memories are evidence with trust state, and skills are versioned procedures.
A compiler selects applicable context; adapters deliver it in forms existing harnesses use.

The [README](../../README.md#available-now-and-ahead) separates implemented foundations from
planned ranking, explanations, knowledge maintenance, and continuity. The [vision](vision.md)
describes how they fit together.

## Build on existing interfaces

Skills use the Agent Skills `SKILL.md` format, with Git for content and history. Harnesses
consume rendered files and MCP tools. GitHub or Linear remains the human tracker for planned
worker coordination. Transcript restoration remains best-effort because it depends on private,
version-sensitive harness storage; structured checkpoints are the intended continuity contract.

## Ownership and license

**Self-hosted, Apache-2.0, whole repository, no paid tier planned.** Substrate is designed for
one organization; multi-team isolation is in scope, hosted multi-tenant SaaS is not.

The adapter and MCP contracts are interfaces others should be able to implement. Apache-2.0
provides a permissive license with an explicit patent grant. A hosted offering would require
revisiting the product boundaries and contributor expectations, not silently changing this
commitment. See [non-goals](non-goals.md).
