import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test("packaged Work publication requires exact human review and creates a separate Personal memory", async ({
  page,
}) => {
  const { spawn, spawnSync } = await import("node:child_process");
  const { mkdtemp, mkdir, readFile, rm } = await import("node:fs/promises");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const { once } = await import("node:events");
  const home = await mkdtemp(join(tmpdir(), "substrate-publication-"));
  const state = join(home, "state");
  const work = join(home, "work");
  const personal = join(home, "personal");
  const workCredential = join(home, "work-session");
  const personalCredential = join(home, "personal-session");
  const firstReview = join(home, "first-review");
  const changedReview = join(home, "changed-review");
  let server;
  let node;
  function command(args, input) {
    const result = spawnSync("./bin/substrate", args, {
      encoding: "utf8",
      timeout: 10000,
      input,
    });
    expect(result.status, result.stderr).toBe(0);
    return result.stdout.trim() ? JSON.parse(result.stdout) : null;
  }
  async function stop(child) {
    if (!child || child.exitCode !== null) return;
    const exited = once(child, "exit");
    child.kill("SIGTERM");
    await exited;
  }
  async function startNode() {
    node = spawn(
      "./bin/substrate",
      ["node", "-state-dir", state, "-index-paused"],
      { stdio: ["ignore", "ignore", "pipe"] },
    );
    await expect
      .poll(
        () =>
          spawnSync(
            "./bin/substrate",
            ["index", "-state-dir", state, "-pause"],
            { timeout: 3000 },
          ).status,
        { timeout: 10000 },
      )
      .toBe(0);
  }
  try {
    for (const path of [work, personal]) {
      await mkdir(path);
      expect(spawnSync("git", ["-C", path, "init", "-q"]).status).toBe(0);
    }
    command(["init", "-state-dir", state, "-owner", "Synthetic owner"]);
    command(["space", "-state-dir", state, "-name", "Work"]);
    for (const [path, space, credential] of [
      [work, "Work", workCredential],
      [personal, "Personal", personalCredential],
    ]) {
      command([
        "register",
        "-state-dir",
        state,
        "-path",
        path,
        "-space",
        space,
      ]);
      command([
        "session",
        "-state-dir",
        state,
        "-path",
        path,
        "-space",
        space,
        "-out",
        credential,
      ]);
    }
    command([
      "publication-policy",
      "-state-dir",
      state,
      "-path",
      work,
      "-action",
      "export",
      "-decision",
      "allow",
    ]);
    command([
      "publication-policy",
      "-state-dir",
      state,
      "-path",
      personal,
      "-action",
      "publish",
      "-decision",
      "allow",
    ]);
    const workContext = command([
      "context",
      "-state-dir",
      state,
      "-path",
      work,
      "-credential",
      workCredential,
    ]);
    const personalContext = command([
      "context",
      "-state-dir",
      state,
      "-path",
      personal,
      "-credential",
      personalCredential,
    ]);
    const token = (await readFile(workCredential, "utf8")).trim();
    await startNode();
    const source = command(
      [
        "capture",
        "-state-dir",
        state,
        "-path",
        work,
        "-credential",
        workCredential,
        "-operation",
        "work-observation",
        "-provenance",
        "Work-only synthetic project evidence",
      ],
      "Work-only confidential particulars. Review build assumptions.",
    );
    server = spawn(
      "./bin/substrate",
      ["serve", "-listen", "127.0.0.1:0", "-state-dir", state],
      { stdio: ["ignore", "ignore", "pipe"] },
    );
    const address = await new Promise((resolve, reject) => {
      let log = "";
      const timeout = setTimeout(
        () => reject(new Error("Server startup timed out")),
        10000,
      );
      server.once("error", reject);
      server.once("exit", () => {
        clearTimeout(timeout);
        reject(new Error("Server exited before startup"));
      });
      server.stderr.on("data", (chunk) => {
        log += chunk;
        const match = log.match(
          /Local browser interface: (http:\/\/127\.0\.0\.1:\d+)/,
        );
        if (match) {
          clearTimeout(timeout);
          resolve(match[1]);
        }
      });
    });
    await page.goto(address);
    await page.getByLabel("Session credential", { exact: true }).fill(token);
    await page
      .getByRole("button", { name: "Inspect context", exact: true })
      .click();
    await expect(page.getByTestId("context-status")).toContainText(
      "Verified Work",
    );
    await page
      .getByRole("button", {
        name: `Inspect memory ${source.artifact_id}`,
        exact: true,
      })
      .click();
    await expect(
      page.getByText(
        "Work-only confidential particulars. Review build assumptions.",
        { exact: true },
      ),
    ).toBeVisible();
    const initialContent =
      "Review build assumptions before drawing a general conclusion.";
    await page
      .getByLabel("Publication text", { exact: true })
      .fill(initialContent);
    await page
      .getByLabel("Personal space ID", { exact: true })
      .fill(personalContext.space_id);
    await page
      .getByLabel("Personal repository ID", { exact: true })
      .fill(personalContext.repo_id);
    const draftResponse = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/publications") &&
        response.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "Save publication draft", exact: true })
      .click();
    const draft = await (await draftResponse).json();
    await expect(page.getByTestId("draft-status")).toContainText(
      "Pending human review",
    );
    const human = page.getByLabel("Human review credential", { exact: true });
    await human.fill(token);
    await page
      .getByRole("button", { name: "Load exact review", exact: true })
      .click();
    await expect(page.getByTestId("review-status")).toContainText("Denied");
    const emptyPersonal = command([
      "search",
      "-state-dir",
      state,
      "-path",
      personal,
      "-credential",
      personalCredential,
    ]);
    expect(emptyPersonal.results).toEqual([]);
    await stop(node);
    node = null;
    command([
      "review-grant",
      "-state-dir",
      state,
      "-path",
      work,
      "-proposal",
      draft.id,
      "-revision",
      draft.revision_id,
      "-destination-path",
      personal,
      "-out",
      firstReview,
    ]);
    await startNode();
    await human.fill((await readFile(firstReview, "utf8")).trim());
    await page
      .getByRole("button", { name: "Load exact review", exact: true })
      .click();
    await expect(page.getByTestId("review-output")).toHaveText(initialContent);
    const confirmation = page.getByLabel(
      "I reviewed the exact output, destination, sources, dependencies, and policies.",
      { exact: true },
    );
    await confirmation.check();
    const changedContent =
      "Recheck the evidence before accepting a build assumption.";
    const changed = await page.evaluate(
      async ({ token, draft, source, destination, content }) => {
        const response = await fetch("/api/publications", {
          method: "POST",
          cache: "no-store",
          credentials: "omit",
          headers: {
            Authorization: `Bearer ${token}`,
            "Content-Type": "application/json",
          },
          body: JSON.stringify({
            operation_id: "revise-publication",
            id: draft.id,
            expected_revision: draft.revision_id,
            content,
            sources: [
              {
                artifact_id: source.artifact_id,
                revision_id: source.revision_id,
              },
            ],
            destination,
          }),
        });
        return { status: response.status, body: await response.json() };
      },
      {
        token,
        draft,
        source,
        destination: {
          space_id: personalContext.space_id,
          repo_id: personalContext.repo_id,
        },
        content: changedContent,
      },
    );
    expect(changed.status).toBe(200);
    await page
      .getByRole("button", { name: "Publish reviewed memory", exact: true })
      .click();
    await expect(page.getByTestId("review-status")).toContainText("Conflict");
    await expect(confirmation).not.toBeChecked();
    const denied = await page.evaluate(
      async ({ token, revision }) => {
        const response = await fetch("/api/publication-review", {
          method: "POST",
          cache: "no-store",
          credentials: "omit",
          headers: {
            Authorization: `Bearer ${token}`,
            "Content-Type": "application/json",
          },
          body: JSON.stringify({
            operation_id: "pretend-human",
            revision_id: revision,
            snapshot: "pretend-human-confirmation",
          }),
        });
        return { status: response.status, body: await response.json() };
      },
      { token, revision: changed.body.revision_id },
    );
    expect(denied.status).toBe(403);
    expect(denied.body.code).toBe("denied");
    await stop(node);
    node = null;
    command([
      "review-grant",
      "-state-dir",
      state,
      "-path",
      work,
      "-proposal",
      draft.id,
      "-revision",
      changed.body.revision_id,
      "-destination-path",
      personal,
      "-out",
      changedReview,
    ]);
    await startNode();
    await human.fill((await readFile(changedReview, "utf8")).trim());
    await expect(page.getByTestId("review-output")).toHaveCount(0);
    await page
      .getByRole("button", { name: "Load exact review", exact: true })
      .click();
    await expect(page.getByTestId("review-output")).toHaveText(changedContent);
    await expect(confirmation).not.toBeChecked();
    const axe = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
      .analyze();
    expect(axe.violations).toEqual([]);
    await confirmation.focus();
    await page.keyboard.press("Space");
    const publicationResponse = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/publication-review") &&
        response.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "Publish reviewed memory", exact: true })
      .focus();
    await page.keyboard.press("Enter");
    const published = await (await publicationResponse).json();
    await expect(page.getByTestId("review-status")).toContainText(
      "Published separate unverified memory",
    );
    expect(published.state).toBe("published");
    const delivered = command([
      "read",
      "-state-dir",
      state,
      "-path",
      personal,
      "-credential",
      personalCredential,
      "-id",
      published.artifact_id,
    ]);
    expect(delivered.revision.content).toBe(changedContent);
    expect(delivered.revision.verification).toBe("unverified");
    expect(delivered.revision.provenance).toBe(
      "Human-reviewed generalized lesson",
    );
    expect(JSON.stringify(delivered)).not.toContain(source.artifact_id);
    expect(JSON.stringify(delivered)).not.toContain(workContext.repo_id);
    expect(JSON.stringify(delivered)).not.toContain(
      "Work-only confidential particulars",
    );
    const personalToken = (await readFile(personalCredential, "utf8")).trim();
    await page
      .getByRole("button", { name: "Clear context", exact: true })
      .click();
    await page
      .getByLabel("Session credential", { exact: true })
      .fill(personalToken);
    await page
      .getByRole("button", { name: "Inspect context", exact: true })
      .click();
    await expect(page.getByTestId("context-status")).toContainText(
      "Verified Personal",
    );
    await page
      .getByRole("button", {
        name: `Inspect memory ${published.artifact_id}`,
        exact: true,
      })
      .click();
    await expect(page.getByText(changedContent, { exact: true })).toBeVisible();
    await expect(
      page.getByText(
        "Work-only confidential particulars. Review build assumptions.",
        { exact: true },
      ),
    ).toHaveCount(0);
  } finally {
    await stop(node);
    await stop(server);
    await rm(home, { recursive: true, force: true });
  }
});
