import { useCallback, useEffect, useRef } from "react";

export async function requestJSON<T>(
  path: string,
  credential: string,
  signal: AbortSignal,
  body?: unknown,
): Promise<T> {
  const response = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    headers: {
      Authorization: `Bearer ${credential.trim()}`,
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
    cache: "no-store",
    credentials: "omit",
  });
  if (!response.ok) {
    const error = await response.json().catch(() => null);
    const state =
      response.status === 403
        ? "Denied"
        : response.status === 409
          ? "Conflict"
          : response.status === 503
            ? "Unavailable"
            : "Error";
    throw new Error(
      `${state}: ${typeof error?.error === "string" ? error.error : "Request failed. Check the local application and credential, then retry."}`,
    );
  }
  return response.json();
}

export function useRequest() {
  const current = useRef<AbortController | null>(null);
  const cancel = useCallback(() => {
    current.current?.abort();
    current.current = null;
  }, []);
  useEffect(() => cancel, [cancel]);
  const start = useCallback(() => {
    if (current.current) return null;
    const controller = new AbortController();
    current.current = controller;
    return controller;
  }, []);
  const finish = useCallback((controller: AbortController) => {
    if (current.current === controller) current.current = null;
  }, []);
  return { start, cancel, finish };
}

export function failure(error: unknown) {
  return error instanceof Error &&
    /^(Denied|Conflict|Unavailable|Error):/.test(error.message)
    ? error.message
    : "Unavailable: check the local application and credential, then retry.";
}
