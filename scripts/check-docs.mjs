import { readdir, readFile, stat } from "node:fs/promises";
import { relative, resolve, sep } from "node:path";
import { parse } from "parse5";

const root = resolve(process.argv[2] ?? "docs/.vitepress/dist");

async function htmlFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const nested = await Promise.all(
    entries.map((entry) =>
      entry.isDirectory()
        ? htmlFiles(resolve(directory, entry.name))
        : entry.name.endsWith(".html")
          ? [resolve(directory, entry.name)]
          : [],
    ),
  );
  return nested.flat();
}

function elements(node) {
  return [node, ...(node.childNodes ?? []).flatMap(elements)];
}

try {
  const pages = new Map();
  for (const file of await htmlFiles(root)) {
    const nodes = elements(parse(await readFile(file, "utf8")));
    pages.set(file, {
      ids: new Set(
        nodes.flatMap((node) =>
          (node.attrs ?? [])
            .filter((attr) => attr.name === "id")
            .map((attr) => attr.value),
        ),
      ),
      links: nodes.flatMap((node) =>
        (node.attrs ?? [])
          .filter((attr) => attr.name === "href" || attr.name === "src")
          .map((attr) => attr.value),
      ),
    });
  }
  if (pages.size === 0)
    throw new Error(
      "No built documentation pages found. Run npm run docs:build first.",
    );
  const failures = [];
  for (const [file, page] of pages) {
    for (const link of page.links) {
      if (/^(?:[a-z][a-z\d+.-]*:|\/\/)/i.test(link)) continue;
      const url = new URL(
        link,
        `http://docs.local/${relative(root, file).split(sep).join("/")}`,
      );
      const name = resolve(root, `.${decodeURIComponent(url.pathname)}`);
      const candidates = [name, `${name}.html`, resolve(name, "index.html")];
      let target;
      for (const candidate of candidates) {
        if (relative(root, candidate).startsWith("..")) continue;
        if (
          await stat(candidate).then(
            (info) => info.isFile(),
            () => false,
          )
        ) {
          target = candidate;
          break;
        }
      }
      const anchor = decodeURIComponent(url.hash.slice(1));
      if (!target || (anchor && !pages.get(target)?.ids.has(anchor))) {
        failures.push(`${relative(root, file)}: broken local link ${link}`);
      }
    }
  }
  if (failures.length) throw new Error([...new Set(failures)].join("\n"));
  console.log(
    `Local pages and anchors verified across ${pages.size} built documentation pages.`,
  );
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
