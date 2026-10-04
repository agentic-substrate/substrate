import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const surfaces = [
  [/^(cmd\/|internal\/|web\/src\/)/, "docs/getting-started.md"],
  [
    /^(Makefile$|package\.json$|go\.mod$|web\/(vite\.config\.ts|tsconfig\.json)$|playwright\.config\.ts$|biome\.json$|scripts\/|\.github\/workflows\/)/,
    "docs/development.md",
  ],
];

export function missingDocs(files, messages) {
  const waived = messages.split("\0").some((message) => {
    const trailers = execFileSync("git", ["interpret-trailers", "--parse"], {
      timeout: 10_000,
      input: message,
      encoding: "utf8",
    });
    return trailers
      .split("\n")
      .some(
        (line) =>
          line.startsWith("Docs-not-needed:") &&
          line.slice("Docs-not-needed:".length).trim().length >= 12,
      );
  });
  if (waived) return [];
  return surfaces
    .filter(
      ([surface, doc]) =>
        files.some((file) => surface.test(file)) && !files.includes(doc),
    )
    .map(([, doc]) => doc);
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  try {
    if (process.argv.length !== 4 || process.argv[2] !== "--base") {
      throw new Error(
        "Usage: node scripts/check-docs-required.mjs --base <Git revision>",
      );
    }
    const git = (...args) =>
      execFileSync("git", args, { encoding: "utf8", timeout: 10_000 });
    const base = git(
      "rev-parse",
      "--verify",
      "--end-of-options",
      `${process.argv[3]}^{commit}`,
    ).trim();
    const tracked = git(
      "diff",
      "--no-renames",
      "--name-only",
      "-z",
      base,
    ).split("\0");
    const untracked = git(
      "ls-files",
      "--others",
      "--exclude-standard",
      "-z",
    ).split("\0");
    const files = [...new Set([...tracked, ...untracked])].filter(Boolean);
    const messages = git("log", "--format=%B%x00", `${base}..HEAD`);
    const missing = missingDocs(
      files.filter((file) => !file.startsWith("docs/") || existsSync(file)),
      messages,
    );
    if (missing.length) {
      throw new Error(
        `Update the paired documentation: ${missing.join(", ")}. Or explain unchanged behavior in a Docs-not-needed: commit trailer (at least 12 characters).`,
      );
    }
    console.log("Documentation decision recorded for every changed surface.");
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
