import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { once } from "node:events";

const executable = "./bin/substrate";
const rejected = spawnSync(executable, ["serve", "-listen", "0.0.0.0:0"], {
  encoding: "utf8",
  timeout: 5_000,
});
assert.equal(rejected.status, 1, "a non-loopback listener must fail");
assert.match(
  rejected.stderr,
  /numeric loopback/,
  "listener denial must explain the boundary",
);

const child = spawn(executable, ["serve", "-listen", "127.0.0.1:0"], {
  stdio: ["ignore", "pipe", "pipe"],
});
let log = "";
const timeout = setTimeout(() => child.kill("SIGKILL"), 10_000);
try {
  const address = await new Promise((accept, reject) => {
    child.once("error", reject);
    child.once("exit", () =>
      reject(new Error(`Server exited before startup: ${log}`)),
    );
    child.stderr.on("data", (chunk) => {
      log += chunk;
      const match = log.match(
        /Local browser interface: (http:\/\/127\.0\.0\.1:\d+)/,
      );
      if (match) accept(match[1]);
    });
  });
  const response = await fetch(`${address}/api/status`, {
    signal: AbortSignal.timeout(3_000),
  });
  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { stage: "bootstrap" });
  const page = await fetch(address, { signal: AbortSignal.timeout(3_000) });
  assert.equal(page.status, 200);
  assert.match(await page.text(), /<title>Substrate<\/title>/);
  const exited = once(child, "exit");
  child.kill("SIGTERM");
  const [code, signal] = await exited;
  assert.equal(code, 0, `Graceful shutdown failed: ${signal ?? log}`);
  console.log(
    "Packaged startup, embedded UI, loopback boundary, and shutdown verified.",
  );
} finally {
  clearTimeout(timeout);
  if (child.exitCode === null) child.kill("SIGKILL");
}
