import { expect, test } from "@playwright/test";

test("packaged browser verifies a real Work credential and observes revocation", async ({
  page,
}) => {
  const { spawn, spawnSync } = await import("node:child_process");
  const { mkdtemp, mkdir, readFile, rm } = await import("node:fs/promises");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const { once } = await import("node:events");
  const home = await mkdtemp(join(tmpdir(), "substrate-browser-"));
  const repo = join(home, "repo");
  const state = join(home, "state");
  const credentialFile = join(home, "session");
  await mkdir(repo);
  expect(spawnSync("git", ["-C", repo, "init", "-q"]).status).toBe(0);
  function command(args) {
    const result = spawnSync("./bin/substrate", args, {
      encoding: "utf8",
      timeout: 5000,
    });
    expect(result.status, result.stderr).toBe(0);
  }
  command(["init", "-state-dir", state, "-owner", "Synthetic owner"]);
  command(["space", "-state-dir", state, "-name", "Work"]);
  command(["register", "-state-dir", state, "-path", repo, "-space", "Work"]);
  command([
    "session",
    "-state-dir",
    state,
    "-path",
    repo,
    "-space",
    "Work",
    "-out",
    credentialFile,
  ]);
  const token = (await readFile(credentialFile, "utf8")).trim();
  const child = spawn(
    "./bin/substrate",
    ["serve", "-listen", "127.0.0.1:0", "-state-dir", state],
    { stdio: ["ignore", "ignore", "pipe"] },
  );
  try {
    const address = await new Promise((resolve, reject) => {
      let log = "";
      const timeout = setTimeout(
        () => reject(new Error("Local server startup timed out")),
        5000,
      );
      child.once("error", reject);
      child.once("exit", () => {
        clearTimeout(timeout);
        reject(new Error("Server exited before startup"));
      });
      child.stderr.on("data", (chunk) => {
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
    const credential = page.getByLabel("Session credential");
    await credential.fill(token);
    await credential.press("Enter");
    await expect(page.getByTestId("context-status")).toContainText(
      "Verified Work",
    );
    await expect(page.getByText(repo, { exact: true })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Clear context" }),
    ).toBeFocused();
    command(["revoke", "-state-dir", state, "-credential", credentialFile]);
    await page.keyboard.press("Enter");
    await expect(credential).toBeFocused();
    await credential.fill(token);
    await credential.press("Enter");
    await expect(page.getByTestId("context-status")).toContainText(
      "Context denied",
    );
    await expect(page.getByText(repo, { exact: true })).toHaveCount(0);
  } finally {
    const exited = once(child, "exit");
    child.kill("SIGTERM");
    await exited;
    await rm(home, { recursive: true, force: true });
  }
});
