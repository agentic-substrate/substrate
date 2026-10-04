import { useEffect, useRef, useState } from "react";

type Context = { space_name: string; checkout: string };

export function SessionContext() {
  const [credential, setCredential] = useState("");
  const [context, setContext] = useState<Context | null>(null);
  const [message, setMessage] = useState(
    "No session bound. Use a credential issued through trusted local setup.",
  );
  const [loading, setLoading] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const clearButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (context) clearButton.current?.focus();
  }, [context]);

  async function bind() {
    if (loading) return;
    setLoading(true);
    setContext(null);
    setMessage("Checking session credential…");
    try {
      const response = await fetch("/api/context", {
        headers: { Authorization: `Bearer ${credential.trim()}` },
        cache: "no-store",
      });
      if (!response.ok) {
        setMessage(
          response.status === 503
            ? "Local authority unavailable. Initialize or repair trusted local setup."
            : "Context denied. Use a trusted credential for a registered checkout and space.",
        );
        return;
      }
      const body: unknown = await response.json();
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
      setMessage(`Verified ${body.space_name} session context.`);
    } catch {
      setMessage(
        "Unable to reach the local authority. Check that it is running, then retry.",
      );
    } finally {
      setCredential("");
      setLoading(false);
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
            This inspection is a snapshot. Future requests require the
            credential again.
          </p>
          <p>
            Registered checkout: <span>{context.checkout}</span>
          </p>
          <button
            type="button"
            ref={clearButton}
            onClick={() => {
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
            onChange={(event) => setCredential(event.target.value)}
            autoComplete="off"
            spellCheck={false}
            required
            aria-describedby="credential-help"
          />
          <p id="credential-help">
            Create a credential with the local session command. It stays in this
            page’s memory and is cleared after each request.
          </p>
          <button type="submit" aria-disabled={loading}>
            Inspect context
          </button>
        </form>
      )}
    </section>
  );
}
