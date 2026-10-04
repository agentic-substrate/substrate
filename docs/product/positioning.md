# Positioning

**Last reviewed:** 2026-10-04 · **Re-read cadence:** quarterly

## One sentence

**Substrate is a self-hosted context layer that helps coding harnesses carry permitted
knowledge, reviewed practices, and eventually unfinished work across sessions and machines.**

Memories, skills, and agent definitions are artifacts. Applicable instructions and preferences
make them useful in a session; provenance, review, and access boundaries keep their reuse
understandable. The [vision](vision.md) describes the complete intended experience.

## What people do today

People repeat project briefings, copy rules and skill folders, keep facts in separate harness
memory stores, and return to the conversation that remembers the task. Shell scripts and file
synchronization reduce copying, but leave people to decide which version is approved, whether
a lesson is still true, and where private material is allowed to appear.

## What the context layer adds

| Existing approach | Intended benefit |
|---|---|
| Copy harness configuration | Resolve the applicable instructions and working preferences from an explicit authority. |
| Store facts independently in each harness | Reuse permitted memories with evidence, verification state, and retained history. |
| Copy skill and agent-definition folders | Deliver an approved, selected source version to the projects and devices where it applies. |
| Stay in the original conversation | Eventually continue through a structured checkpoint across harnesses and machines. |
| Synchronize everything the user can access | Make audience, placement, processing, and cross-space publication explicit and inspectable. |

The goal is less repeated explanation and fewer conflicting copies, with control over how
knowledge is reused. This is an intended product advantage; the current checkout provides a
bootstrap shell, not the artifact runtime. The [getting started guide](../getting-started.md)
describes what runs today.

## Who benefits first

Start with one developer using several harnesses on one Linux or WSL node. Work and Personal
projects need useful context that survives tool changes without mixing their information.
Multiple WSL instances and machines follow; teams and organizations add shared practice,
independent membership, and administration while retaining restricted and personal context.

Straightforward joins and administration are major outcomes at every scale. A person managing
their own systems and an organization administrator both need reusable trusted bindings,
a view of who receives which context, and understandable device replacement and recovery.

## Build beneath existing tools

Substrate is context infrastructure rather than another coding harness, IDE, or issue tracker.
The intended CLI and MCP interfaces give existing harnesses access to governed artifacts.
Validated native adapters can deliver files and approved definitions where they preserve the
same permissions and selected versions. Skills use the Agent Skills `SKILL.md` format, with
Git for source content and history.

Structured checkpoints are the planned continuity contract. Transcript restoration is an
optional convenience tied to validated clients, rather than a requirement for portability.
The [first release](first-release.md) proves local artifact reuse and boundaries before that
full continuity experience is built.

## Ownership and license

**Self-hosted, Apache-2.0, whole repository, no paid tier planned.** Substrate supports personal,
team, and organization context spaces with explicit sharing boundaries. Hosted multi-customer
SaaS remains outside scope.

The adapter and MCP contracts are interfaces others should be able to implement. Apache-2.0
provides a permissive license with an explicit patent grant. A hosted offering would require
revisiting product boundaries and contributor expectations, not silently changing this
commitment. See [non-goals](non-goals.md).
