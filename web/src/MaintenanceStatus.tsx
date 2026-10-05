import type { Maintenance } from "./artifactTypes";

function validMaintenance(value: unknown): value is Maintenance {
  if (typeof value !== "object" || value === null) return false;
  const status = value as Partial<Maintenance>;
  if (
    status.mode !== "lexical" ||
    typeof status.paused !== "boolean" ||
    !["ready", "pending", "deferred", "failed"].includes(status.state ?? "") ||
    typeof status.coverage !== "object" ||
    status.coverage === null
  )
    return false;
  if (
    ![
      status.queued,
      status.deferred,
      status.failed,
      status.coverage.eligible,
      status.coverage.indexed,
      status.coverage.pending,
      status.coverage.limited,
    ].every(
      (count) =>
        typeof count === "number" && Number.isSafeInteger(count) && count >= 0,
    )
  )
    return false;
  const { eligible, indexed, pending, limited } = status.coverage;
  const state =
    status.failed !== 0
      ? "failed"
      : status.queued !== 0
        ? "pending"
        : status.deferred !== 0
          ? "deferred"
          : "ready";
  return (
    indexed <= eligible &&
    pending === eligible - indexed &&
    limited <= indexed &&
    status.state === state
  );
}

export function MaintenanceStatus({ maintenance }: { maintenance: unknown }) {
  const status = validMaintenance(maintenance) ? maintenance : null;
  const coverage = status?.coverage;
  const coverageMessage = !coverage
    ? ""
    : coverage.eligible === 0
      ? "Empty lexical coverage. No eligible current revisions in this context."
      : coverage.indexed === 0
        ? "Empty lexical coverage. No current revisions indexed yet; exact reads remain available."
        : coverage.indexed === coverage.eligible &&
            coverage.pending === 0 &&
            coverage.limited === 0
          ? "Complete lexical coverage for eligible current revisions in this context."
          : "Partial lexical coverage. Pending or limited revisions can leave text outside search results; exact reads remain available.";

  return (
    <section className="maintenance" aria-labelledby="maintenance-heading">
      <h3 id="maintenance-heading">Retrieval and maintenance</h3>
      {status ? (
        <>
          <p>
            Queue and coverage counts include the full permitted owner, space,
            and repository, including records beyond the inventory limit.
            Indexing controls are available through the trusted local owner CLI.
          </p>
          <dl className="maintenance-counts">
            <dt>Retrieval mode</dt>
            <dd>Lexical</dd>
            <dt>Installation indexing pause</dt>
            <dd>{status.paused ? "Paused" : "Not paused"}</dd>
            <dt>Scoped queue state</dt>
            <dd>
              {status.state === "ready"
                ? "Ready"
                : status.state === "pending"
                  ? "Pending"
                  : status.state === "deferred"
                    ? "Deferred"
                    : "Failed"}
            </dd>
            <dt>Queued work</dt>
            <dd>{status.queued}</dd>
            <dt>Deferred work</dt>
            <dd>{status.deferred}</dd>
            <dt>Failures</dt>
            <dd>{status.failed}</dd>
          </dl>
          <h4>Lexical search coverage</h4>
          <p role="status">{coverageMessage}</p>
          <dl className="maintenance-counts">
            <dt>Eligible current revisions</dt>
            <dd>{status.coverage.eligible}</dd>
            <dt>Indexed revisions</dt>
            <dd>{status.coverage.indexed}</dd>
            <dt>Pending revisions</dt>
            <dd>{status.coverage.pending}</dd>
            <dt>Limited revisions</dt>
            <dd>{status.coverage.limited}</dd>
          </dl>
        </>
      ) : (
        <p role="status">
          Unavailable: maintenance status is missing or invalid. Refresh
          artifacts; if this persists, repair the local application.
        </p>
      )}
    </section>
  );
}
