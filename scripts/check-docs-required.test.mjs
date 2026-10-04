import assert from "node:assert/strict";
import test from "node:test";
import { missingDocs } from "./check-docs-required.mjs";

test("a changed surface names the guide to update", () => {
  assert.deepEqual(missingDocs(["internal/server/server.go"], ""), [
    "docs/getting-started.md",
  ]);
  assert.deepEqual(
    missingDocs(["internal/server/server.go", "docs/getting-started.md"], ""),
    [],
  );
});

test("a waiver requires a real explanation in a trailer", () => {
  const files = ["scripts/check-docs.mjs"];
  assert.deepEqual(
    missingDocs(files, "Refactor\n\nDocs-not-needed:              "),
    ["docs/development.md"],
  );
  assert.deepEqual(
    missingDocs(
      files,
      "Refactor\n\nDocs-not-needed: A long line in the body is not a trailer.\n\nMore explanation follows.",
    ),
    ["docs/development.md"],
  );
  assert.deepEqual(missingDocs(files, "Docs-not-needed: nope"), [
    "docs/development.md",
  ]);
  assert.deepEqual(
    missingDocs(
      files,
      "Subject mentions Docs-not-needed: This is a sufficiently long reason.",
    ),
    ["docs/development.md"],
  );
  assert.deepEqual(
    missingDocs(
      files,
      "Refactor\n\nDocs-not-needed: Existing documented behavior is unchanged.",
    ),
    [],
  );
});

test("unrelated docs cannot satisfy multiple changed surfaces", () => {
  assert.deepEqual(
    missingDocs(["web/src/App.tsx", "Makefile", "docs/product/vision.md"], ""),
    ["docs/getting-started.md", "docs/development.md"],
  );
  assert.deepEqual(missingDocs(["docs/product/vision.md"], ""), []);
});
