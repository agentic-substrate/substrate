import { useCallback, useEffect, useRef, useState } from "react";
import { failure, requestJSON, useRequest } from "./api";
import type { ArtifactDetail, Inventory, Proposal } from "./artifactTypes";
import { RevisionContent } from "./RevisionContent";

export function ArtifactBrowser({ credential }: { credential: string }) {
  const [inventory, setInventory] = useState<Inventory | null>(null);
  const [detail, setDetail] = useState<ArtifactDetail | null>(null);
  const [message, setMessage] = useState("Loading permitted artifacts…");
  const [loading, setLoading] = useState(false);
  const [content, setContent] = useState("");
  const [space, setSpace] = useState("");
  const [repository, setRepository] = useState("");
  const [draftMessage, setDraftMessage] = useState(
    "No publication draft saved.",
  );
  const [draft, setDraft] = useState<Proposal | null>(null);
  const operation = useRef<string | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const { start, cancel, finish } = useRequest();

  const refresh = useCallback(async () => {
    const controller = start();
    if (!controller) return;
    setLoading(true);
    setInventory(null);
    setDetail(null);
    setDraft(null);
    setContent("");
    operation.current = null;
    setMessage("Loading permitted artifacts…");
    try {
      const result = await requestJSON<Inventory>(
        "/api/artifacts",
        credential,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      if (
        !result.context ||
        !Array.isArray(result.artifacts) ||
        !Array.isArray(result.proposals)
      )
        throw new Error(
          "Unavailable: invalid artifact inventory. Repair the local application, then retry.",
        );
      setInventory(result);
      setMessage(
        result.artifacts.length
          ? "Permitted artifact inventory loaded."
          : "Empty: no artifacts in this permitted context. Capture an observation through the CLI.",
      );
    } catch (error) {
      if (!controller.signal.aborted) setMessage(failure(error));
    } finally {
      finish(controller);
      if (!controller.signal.aborted) setLoading(false);
    }
  }, [credential, start, finish]);
  useEffect(() => {
    void refresh();
    return cancel;
  }, [refresh, cancel]);

  async function inspect(id: string) {
    const controller = start();
    if (!controller) return;
    setLoading(true);
    setDetail(null);
    setDraft(null);
    setContent("");
    operation.current = null;
    setDraftMessage("No publication draft saved.");
    setMessage("Loading authorized revision history…");
    try {
      const result = await requestJSON<ArtifactDetail>(
        `/api/artifacts/${encodeURIComponent(id)}`,
        credential,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      if (
        !result.artifact ||
        !Array.isArray(result.artifact.revisions) ||
        !Array.isArray(result.choices) ||
        !Array.isArray(result.actions)
      )
        throw new Error(
          "Unavailable: invalid artifact inspection. Retry after repairing the application.",
        );
      setDetail(result);
      setMessage("Authorized revision history loaded.");
      requestAnimationFrame(() => {
        if (!controller.signal.aborted) heading.current?.focus();
      });
    } catch (error) {
      if (!controller.signal.aborted) setMessage(failure(error));
    } finally {
      finish(controller);
      if (!controller.signal.aborted) setLoading(false);
    }
  }

  function changeDraft(change: () => void) {
    cancel();
    setLoading(false);
    setDraft(null);
    operation.current = null;
    setDraftMessage("Draft changed. Save it for a new exact-content review.");
    change();
  }

  async function saveDraft() {
    if (!detail?.artifact.head_revision) return;
    const controller = start();
    if (!controller) return;
    operation.current ||= crypto.randomUUID();
    setLoading(true);
    setDraftMessage("Saving publication draft…");
    try {
      const result = await requestJSON<Proposal>(
        "/api/publications",
        credential,
        controller.signal,
        {
          operation_id: operation.current,
          content,
          sources: [
            {
              artifact_id: detail.artifact.id,
              revision_id: detail.artifact.head_revision,
            },
          ],
          destination: { space_id: space.trim(), repo_id: repository.trim() },
        },
      );
      if (controller.signal.aborted) return;
      setDraft(result);
      setDraftMessage(
        `Pending human review: ${result.state}. Draft ${result.id} revision ${result.revision_id}. Nothing has been published.`,
      );
    } catch (error) {
      if (!controller.signal.aborted) setDraftMessage(failure(error));
    } finally {
      finish(controller);
      if (!controller.signal.aborted) setLoading(false);
    }
  }

  const publication = detail?.actions.find(
    (action) => action.action === "publication",
  );
  const canDraft =
    publication?.state === "approval-required" ||
    publication?.state === "allowed";
  return (
    <section aria-labelledby="artifacts-heading">
      <h2 id="artifacts-heading">Artifacts in this context</h2>
      <p role="status" data-testid="artifact-status">
        {message}
      </p>
      <button
        type="button"
        aria-disabled={loading}
        onClick={() => void refresh()}
      >
        Refresh artifacts
      </button>
      {inventory && (
        <>
          <h3>Trusted binding</h3>
          <p>
            This view shows up to 100 artifacts and 100 proposals. Use scoped
            CLI search and inspection for additional records.
          </p>
          <p>
            Active space: {inventory.context.space_name}. Space ID:{" "}
            {inventory.context.space_id}. Repository:{" "}
            {inventory.context.repo_id}. Checkout: {inventory.context.checkout}.
          </p>
          <ul className="artifact-list">
            {inventory.artifacts.map((artifact) => (
              <li key={artifact.id}>
                <button
                  type="button"
                  aria-disabled={loading}
                  onClick={() => void inspect(artifact.id)}
                >
                  Inspect {artifact.kind} {artifact.id}
                </button>
                <p>
                  Lifecycle: {artifact.lifecycle}. Selected head:{" "}
                  {artifact.head_revision || "None; approval pending"}.
                </p>
              </li>
            ))}
          </ul>
          <h3>Publication proposals</h3>
          {inventory.proposals.length ? (
            <ul>
              {inventory.proposals.map((proposal) => (
                <li key={proposal.id}>
                  {proposal.id}, revision {proposal.revision_id}:{" "}
                  {proposal.state}. Destination space:{" "}
                  {proposal.destination.space_id}, repository:{" "}
                  {proposal.destination.repo_id}.
                </li>
              ))}
            </ul>
          ) : (
            <p>No saved proposals in this source context.</p>
          )}
        </>
      )}
      {detail && (
        <>
          <h3 ref={heading} tabIndex={-1}>
            Artifact details
          </h3>
          <p>
            {detail.artifact.kind} artifact {detail.artifact.id}. Lifecycle:{" "}
            {detail.artifact.lifecycle}. Space: {detail.artifact.space_id}.
            Repository: {detail.artifact.repo_id}. Selected head:{" "}
            {detail.artifact.head_revision || "None; approval pending"}.
          </p>
          <h4>Permitted actions and limits</h4>
          <ul>
            {detail.actions.map((action) => (
              <li key={action.action}>
                {action.action}: {action.state}. <span>{action.reason}</span>
              </li>
            ))}
          </ul>
          {detail.choices.map((choice) => (
            <p key={choice.artifact_id}>
              Qualified identity: {choice.qualified}. Alias:{" "}
              {choice.alias || "None"}. Selection: {choice.state}.{" "}
              {choice.reason} Override target: {choice.overrides || "None"}.
              Pinned default: {choice.override_revision || "None"}.
            </p>
          ))}
          <h4>All authorized revisions</h4>
          {detail.artifact.revisions.map((revision) => (
            <RevisionContent key={revision.id} revision={revision} />
          ))}
          <h3>Draft a publication</h3>
          <p>
            Source remains in {inventory?.context.space_name}. This creates a
            separate unverified memory in Personal only after exact-content
            human review.
          </p>
          <p>
            Publication: {publication?.state || "denied"}. Inspect the action
            limits above before drafting.
          </p>
          {canDraft && detail.artifact.head_revision && (
            <form
              onSubmit={(event) => {
                event.preventDefault();
                void saveDraft();
              }}
            >
              <p>Exact source revision: {detail.artifact.head_revision}.</p>
              <label htmlFor="publication-text">Publication text</label>
              <textarea
                id="publication-text"
                value={content}
                required
                maxLength={1048576}
                rows={6}
                onChange={(event) =>
                  changeDraft(() => setContent(event.target.value))
                }
              />
              <p id="destination-help">
                Enter Personal destination IDs from trusted CLI binding
                inventory. This Work session cannot enumerate or switch into
                Personal bindings.
              </p>
              <label htmlFor="personal-space">Personal space ID</label>
              <input
                id="personal-space"
                value={space}
                required
                aria-describedby="destination-help"
                onChange={(event) =>
                  changeDraft(() => setSpace(event.target.value))
                }
              />
              <label htmlFor="personal-repository">
                Personal repository ID
              </label>
              <input
                id="personal-repository"
                value={repository}
                required
                aria-describedby="destination-help"
                onChange={(event) =>
                  changeDraft(() => setRepository(event.target.value))
                }
              />
              <button type="submit" aria-disabled={loading}>
                Save publication draft
              </button>
            </form>
          )}
          <p role="status" data-testid="draft-status">
            {draftMessage}
          </p>
          {draft && (
            <p>
              Saved output: {draft.content}. Destination space:{" "}
              {draft.destination.space_id}, repository:{" "}
              {draft.destination.repo_id}. Issue a dedicated review credential
              through trusted owner setup.
            </p>
          )}
        </>
      )}
    </section>
  );
}
