import { useCallback, useEffect, useState } from "react";
import { ArtifactBrowser } from "./ArtifactBrowser";
import { PublicationReview } from "./PublicationReview";
import { SessionContext } from "./SessionContext";

type Status = "loading" | "connected" | "error";

export function App() {
  const [status, setStatus] = useState<Status>("loading");
  const [credential, setCredential] = useState<string | null>(null);
  const [contextVersion, setContextVersion] = useState(0);

  const refresh = useCallback(async (signal?: AbortSignal) => {
    setStatus("loading");
    try {
      const response = await fetch("/api/status", { signal });
      if (!response.ok) throw new Error("Status request failed");
      const body: unknown = await response.json();
      if (
        typeof body !== "object" ||
        body === null ||
        !("stage" in body) ||
        body.stage !== "bootstrap"
      ) {
        throw new Error("Unexpected status response");
      }
      setStatus("connected");
    } catch {
      if (!signal?.aborted) setStatus("error");
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void refresh(controller.signal);
    return () => controller.abort();
  }, [refresh]);

  return (
    <main>
      <h1>Substrate</h1>
      <p className="intro">
        Project knowledge that carries between coding sessions.
      </p>
      <section aria-labelledby="status-heading">
        <h2 id="status-heading">Application status</h2>
        <p role="status">
          {status === "loading"
            ? "Checking the local application…"
            : status === "connected"
              ? "Connected to the local application."
              : "Unable to reach the local application. Check that it is running, then retry."}
        </p>
        <button
          type="button"
          onClick={() => {
            if (status !== "loading") void refresh();
          }}
          aria-disabled={status === "loading"}
        >
          Refresh status
        </button>
      </section>
      <SessionContext
        onCredential={(token) => {
          setCredential(token);
          setContextVersion((version) => version + 1);
        }}
      />
      {credential && (
        <ArtifactBrowser key={contextVersion} credential={credential} />
      )}
      <PublicationReview key={`review-${contextVersion}`} />
      <section aria-labelledby="scope-heading">
        <h2 id="scope-heading">Available in this checkout</h2>
        <p>
          Trusted local owner setup, explicit space and repository bindings, and
          scoped session credentials are available through the CLI. The CLI can
          also save, search, and read permitted observations and approved Git
          snapshots. Scoped MCP tools connect harnesses to the local node.
        </p>
        <p>
          Inspect permitted artifact history and exact-content publication
          review here. Native activation and synchronization remain unavailable.
        </p>
      </section>
    </main>
  );
}
