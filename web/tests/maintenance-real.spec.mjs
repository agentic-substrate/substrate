import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test("packaged maintenance survives restart, isolates Work counts and completes an explicit owner rebuild", async ({
  page,
}) => {
  const { spawn, spawnSync } = await import("node:child_process");
  const { once } = await import("node:events");
  const { mkdtemp, mkdir, readFile, rm } = await import("node:fs/promises");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const home = await mkdtemp(join(tmpdir(), "substrate-maintenance-"));
  const state = join(home, "state");
  const work = join(home, "work");
  const personal = join(home, "personal");
  const workCredential = join(home, "work-session");
  const personalCredential = join(home, "personal-session");
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

  async function startNode(paused = false) {
    node = spawn(
      "./bin/substrate",
      ["node", "-state-dir", state, ...(paused ? ["-index-paused"] : [])],
      { stdio: ["ignore", "ignore", "pipe"] },
    );
    await expect
      .poll(
        () =>
          spawnSync(
            "./bin/substrate",
            ["index", "-state-dir", state, "-status"],
            { timeout: 3000 },
          ).status,
      )
      .toBe(0);
  }

  async function bind(credential) {
    await page
      .getByLabel("Session credential", { exact: true })
      .fill(credential);
    await page
      .getByRole("button", { name: "Inspect context", exact: true })
      .click();
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
    await startNode(true);
    const receipt = command(
      [
        "capture",
        "-state-dir",
        state,
        "-path",
        work,
        "-credential",
        workCredential,
        "-operation",
        "maintenance-observation",
        "-provenance",
        "Synthetic Work evidence",
      ],
      "Work-only lexical observation.",
    );

    server = spawn(
      "./bin/substrate",
      ["serve", "-listen", "127.0.0.1:0", "-state-dir", state],
      {
        stdio: ["ignore", "ignore", "pipe"],
      },
    );
    const address = await new Promise((resolve, reject) => {
      let log = "";
      const timeout = setTimeout(
        () => reject(new Error("Local browser startup timed out")),
        10000,
      );
      server.once("error", reject);
      server.once("exit", () => {
        clearTimeout(timeout);
        reject(new Error("Local browser exited before startup"));
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
    await bind((await readFile(workCredential, "utf8")).trim());
    const status = page.getByRole("region", {
      name: "Retrieval and maintenance",
    });
    await expect(status).toContainText("Paused");
    await expect(status).toContainText("Pending");
    await expect(status).toContainText("No current revisions indexed yet");
    await expect(
      status.getByText("Queued work", { exact: true }).locator("+ dd"),
    ).toHaveText("1");

    command(["index", "-state-dir", state, "-rebuild"]);
    await stop(node);
    await startNode();
    const refresh = page.getByRole("button", {
      name: "Refresh artifacts",
      exact: true,
    });
    await refresh.click();
    await expect(status).toContainText("Paused");
    await expect(
      status.getByText("Scoped queue state", { exact: true }).locator("+ dd"),
    ).toHaveText("Deferred");
    await expect(
      status.getByText("Queued work", { exact: true }).locator("+ dd"),
    ).toHaveText("0");
    await expect(
      status.getByText("Deferred work", { exact: true }).locator("+ dd"),
    ).toHaveText("1");

    await page
      .getByRole("button", { name: "Clear context", exact: true })
      .click();
    await bind((await readFile(personalCredential, "utf8")).trim());
    await expect(page.getByTestId("context-status")).toContainText(
      "Verified Personal",
    );
    await expect(status).toContainText("No eligible current revisions");
    await expect(
      status.getByText("Deferred work", { exact: true }).locator("+ dd"),
    ).toHaveText("0");
    await expect(
      page.getByRole("button", {
        name: `Inspect memory ${receipt.artifact_id}`,
        exact: true,
      }),
    ).toHaveCount(0);

    await page
      .getByRole("button", { name: "Clear context", exact: true })
      .click();
    await bind((await readFile(workCredential, "utf8")).trim());
    await expect(status).toContainText("Paused");
    command(["index", "-state-dir", state, "-pause=false"]);
    command(["index", "-state-dir", state, "-run"]);
    await refresh.focus();
    await page.keyboard.press("Enter");
    await expect(status).toContainText("Not paused");
    await expect(
      status.getByText("Scoped queue state", { exact: true }).locator("+ dd"),
    ).toHaveText("Ready");
    await expect(status).toContainText("Complete lexical coverage");
    await expect(refresh).toBeFocused();
    const scan = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
      .analyze();
    expect(scan.violations).toEqual([]);
  } finally {
    await stop(server);
    await stop(node);
    await rm(home, { recursive: true, force: true });
  }
});
