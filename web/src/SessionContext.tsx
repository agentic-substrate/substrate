import { useEffect, useRef, useState } from "react";
import { useRequest } from "./api";

type Context = { space_name: string; checkout: string };

export function SessionContext({
  onCredential,
}: {
  onCredential: (credential: string | null) => void;
}) {
  const [credential, setCredential] = useState("");
  const [context, setContext] = useState<Context | null>(null);
  const [message, setMessage] = useState(
    "No session bound. Use a credential issued through trusted local setup.",
  );
  const [loading, setLoading] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const clearButton = useRef<HTMLButtonElement>(null);
  const { start, cancel, finish } = useRequest();
  useEffect(() => {
    if (context) clearButton.current?.focus();
  }, [context]);

  async function bind() {
    const controller = start();
    if (!controller) return;
    const token = credential.trim();
    onCredential(null);
    setLoading(true);
    setContext(null);
    setMessage("Checking session credential…");
    try {
      const response = await fetch("/api/context", {
        headers: { Authorization: `Bearer ${token}` },
        cache: "no-store",
        credentials: "omit",
        signal: controller.signal,
      });
      if (controller.signal.aborted) return;
      if (!response.ok) {
        setMessage(
          response.status === 503
            ? "Local authority unavailable. Initialize or repair trusted local setup."
            : "Context denied. Use a trusted credential for a registered checkout and space.",
        );
        return;
      }
      const body: unknown = await response.json();
      if (controller.signal.aborted) return;
      if (
        typeof body !== "object" ||
        body === null ||
        !("space_name" in body) ||
        typeof body.space_name !== "string" ||
        !("checkout" in body) ||
        typeof body.checkout !== "string"
      )
        throw new Error("Invalid context");
      setContext({ space_name: body.space_name, checkout: body.checkout });
      onCredential(token);
      setMessage(`Verified ${body.space_name} session context.`);
    } catch {
      if (!controller.signal.aborted)
        setMessage(
          "Unable to reach the local authority. Check that it is running, then retry.",
        );
    } finally {
      finish(controller);
      if (!controller.signal.aborted) {
        setCredential("");
        setLoading(false);
      }
    }
  }

  return (
    <section aria-labelledby="context-heading">
      <h2 id="context-heading">Session context</h2>
      <p role="status" data-testid="context-status">
        {message}
      </p>
      {context ? (
        <>
          <p>
            This binding is a snapshot. Artifact requests recheck the
            credential, held only in page memory until you clear this context.
          </p>
          <p>
            Registered checkout: <span>{context.checkout}</span>
          </p>
          <button
            type="button"
            ref={clearButton}
            onClick={() => {
              cancel();
              onCredential(null);
              setCredential("");
              setLoading(false);
              setContext(null);
              setMessage(
                "No session bound. Use a credential issued through trusted local setup.",
              );
              requestAnimationFrame(() => input.current?.focus());
            }}
          >
            Clear context
          </button>
        </>
      ) : (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void bind();
          }}
        >
          <label htmlFor="session-credential">Session credential</label>
          <input
            ref={input}
            id="session-credential"
            type="password"
            value={credential}
            onChange={(event) => {
              cancel();
              onCredential(null);
              setContext(null);
              setLoading(false);
              setCredential(event.target.value);
              setMessage(
                "No session bound. Inspect this credential to establish context.",
              );
            }}
            autoComplete="off"
            spellCheck={false}
            required
            aria-describedby="credential-help"
          />
          <p id="credential-help">
            Create a credential with the local session command. It stays in this
            page’s memory until context is cleared; each request checks current
            authority.
          </p>
          <button type="submit" aria-disabled={loading}>
            Inspect context
          </button>
          <button
            type="button"
            onClick={() => {
              cancel();
              onCredential(null);
              setContext(null);
              setCredential("");
              setLoading(false);
              setMessage(
                "No session bound. Use a credential issued through trusted local setup.",
              );
              input.current?.focus();
            }}
          >
            Clear credential
          </button>
        </form>
      )}
    </section>
  );
}
