import { useCallback, useEffect, useState } from "react";

type Status = "loading" | "connected" | "error";

export function App() {
  const [status, setStatus] = useState<Status>("loading");

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
      <p className="eyebrow">Local application</p>
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
      <section aria-labelledby="scope-heading">
        <h2 id="scope-heading">Available in this checkout</h2>
        <p>
          The application shell and development checks are ready for building
          the local artifact layer.
        </p>
        <p>
          Artifact storage, scoped MCP tools, publication review, and
          synchronization are planned work.
        </p>
      </section>
    </main>
  );
}
