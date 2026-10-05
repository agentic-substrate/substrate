import { useRef, useState } from "react";
import { failure, requestJSON, useRequest } from "./api";
import type { Review } from "./artifactTypes";
import { RevisionContent } from "./RevisionContent";

export function PublicationReview() {
  const [credential, setCredential] = useState("");
  const [review, setReview] = useState<Review | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState(
    "No human review loaded. Use a dedicated owner-issued review credential.",
  );
  const input = useRef<HTMLInputElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const operation = useRef<string | null>(null);
  const reviewIdentity = useRef<string | null>(null);
  const { start, cancel, finish } = useRequest();

  function reset() {
    cancel();
    setLoading(false);
    setReview(null);
    setConfirmed(false);
    operation.current = null;
    reviewIdentity.current = null;
  }

  async function load() {
    const controller = start();
    if (!controller) return;
    setLoading(true);
    setReview(null);
    setConfirmed(false);
    setMessage("Loading exact publication review…");
    try {
      const result = await requestJSON<Review>(
        "/api/publication-review",
        credential,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      if (
        !result.proposal ||
        typeof result.proposal.content !== "string" ||
        !result.proposal.revision_id ||
        !result.snapshot ||
        !result.source_context ||
        !result.destination_context ||
        !Array.isArray(result.sources) ||
        !Array.isArray(result.policies) ||
        !Array.isArray(result.dependencies)
      )
        throw new Error(
          "Unavailable: review snapshot is incomplete. Reload after repairing the local application.",
        );
      setReview(result);
      const identity = `${result.proposal.revision_id}/${result.snapshot}`;
      if (reviewIdentity.current !== identity) operation.current = null;
      reviewIdentity.current = identity;
      setMessage(
        "Pending confirmation: review the complete exact output and its destination before publishing.",
      );
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

  async function publish() {
    if (
      !review ||
      !confirmed ||
      review.policies.some((policy) => !policy.allowed)
    )
      return;
    const controller = start();
    if (!controller) return;
    operation.current ||= crypto.randomUUID();
    setLoading(true);
    setMessage("Submitting exact-content publication approval…");
    try {
      const receipt = await requestJSON<{
        artifact_id: string;
        revision_id: string;
        state: string;
      }>("/api/publication-review", credential, controller.signal, {
        operation_id: operation.current,
        revision_id: review.proposal.revision_id,
        snapshot: review.snapshot,
      });
      if (controller.signal.aborted) return;
      if (
        receipt.state !== "published" ||
        !receipt.artifact_id ||
        !receipt.revision_id
      )
        throw new Error(
          "Unavailable: no publication receipt received. Retry the same exact approval.",
        );
      setReview(null);
      setConfirmed(false);
      setMessage(
        `Published separate unverified memory ${receipt.artifact_id}, revision ${receipt.revision_id}. The source remains in its original context.`,
      );
    } catch (error) {
      if (!controller.signal.aborted) {
        setConfirmed(false);
        setMessage(failure(error));
      }
    } finally {
      finish(controller);
      if (!controller.signal.aborted) setLoading(false);
    }
  }

  const denied = review?.policies.some((policy) => !policy.allowed);
  return (
    <section aria-labelledby="review-heading">
      <h2 id="review-heading">Human publication review</h2>
      <p>
        Each new output needs a dedicated review credential from trusted owner
        setup. An ordinary session credential cannot approve publication.
      </p>
      <p role="status" data-testid="review-status">
        {message}
      </p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void load();
        }}
      >
        <label htmlFor="human-review-credential">Human review credential</label>
        <input
          ref={input}
          id="human-review-credential"
          type="password"
          value={credential}
          required
          autoComplete="off"
          spellCheck={false}
          aria-describedby="review-credential-help"
          onChange={(event) => {
            reset();
            setCredential(event.target.value);
            setMessage(
              "Review cleared. Load the exact review with this credential.",
            );
          }}
        />
        <p id="review-credential-help">
          The credential stays in page memory until changed or cleared. It is
          never stored in cookies, a URL, or browser storage.
        </p>
        <div className="actions">
          <button type="submit" aria-disabled={loading}>
            Load exact review
          </button>
          <button
            type="button"
            onClick={() => {
              reset();
              setCredential("");
              setMessage(
                "Human review cleared. No approval is pending in this page.",
              );
              input.current?.focus();
            }}
          >
            Clear human review
          </button>
        </div>
      </form>
      {review && (
        <>
          <h3 ref={heading} tabIndex={-1}>
            Exact publication output
          </h3>
          <pre data-testid="review-output">{review.proposal.content}</pre>
          <p>
            Proposal {review.proposal.id}, revision{" "}
            {review.proposal.revision_id}: {review.proposal.state}.
          </p>
          <h3>Source context</h3>
          <p>
            {review.source_context.space_name}. Space:{" "}
            {review.source_context.space_id}. Repository:{" "}
            {review.source_context.repo_id}. Checkout:{" "}
            {review.source_context.checkout}.
          </p>
          <h3>Destination context</h3>
          <p>
            {review.destination_context.space_name}. Space:{" "}
            {review.destination_context.space_id}. Repository:{" "}
            {review.destination_context.repo_id}. Checkout:{" "}
            {review.destination_context.checkout}.
          </p>
          <dl>
            <dt>Audience</dt>
            <dd>{review.audience}</dd>
            <dt>Placement</dt>
            <dd>{review.placement}</dd>
            <dt>Published provenance</dt>
            <dd>{review.provenance}</dd>
            <dt>Review snapshot</dt>
            <dd>{review.snapshot}</dd>
          </dl>
          <h3>Exact source and dependency inventory</h3>
          {review.sources.map((source) => (
            <div key={`${source.artifact_id}/${source.revision.id}`}>
              <p>
                {source.kind} artifact {source.artifact_id}.{" "}
                {source.choice?.qualified &&
                  `Selection: ${source.choice.qualified}, ${source.choice.state}. ${source.choice.reason}`}
              </p>
              <RevisionContent revision={source.revision} />
            </div>
          ))}
          {review.dependencies.length ? (
            review.dependencies.map((dependency) => (
              <div key={dependency.path}>
                <p>
                  Path: {dependency.path}. Blob: {dependency.blob}
                </p>
                <pre>{dependency.content}</pre>
              </div>
            ))
          ) : (
            <p>No additional dependencies.</p>
          )}
          <h3>Current policies</h3>
          <ul>
            {review.policies.map((policy) => (
              <li key={`${policy.scope}/${policy.id}/${policy.action}`}>
                {policy.scope} {policy.id}: {policy.action}{" "}
                {policy.allowed ? "allowed" : "denied"}, policy epoch{" "}
                {policy.epoch}.
              </li>
            ))}
          </ul>
          {denied && (
            <p>
              Denied: a mandatory policy blocks publication. Confirmation cannot
              override it.
            </p>
          )}
          <form
            onSubmit={(event) => {
              event.preventDefault();
              void publish();
            }}
          >
            <label className="confirmation" htmlFor="review-confirmation">
              <input
                id="review-confirmation"
                type="checkbox"
                checked={confirmed}
                onChange={(event) => setConfirmed(event.target.checked)}
              />
              <span>
                I reviewed the exact output, destination, sources, dependencies,
                and policies.
              </span>
            </label>
            <button
              type="submit"
              aria-disabled={loading || !confirmed || denied}
            >
              Publish reviewed memory
            </button>
          </form>
        </>
      )}
    </section>
  );
}
