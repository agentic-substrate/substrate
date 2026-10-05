import AxeBuilder from "@axe-core/playwright";
import type { Page } from "@playwright/test";
import { expect, test } from "@playwright/test";

const context = {
  owner_id: "owner",
  space_id: "work",
  space_name: "Work",
  repo_id: "work-repo",
  checkout: "/synthetic/work",
};
const revision = {
  id: "current-memory",
  base: "original-memory",
  content: "Exact Work evidence, including confidential particulars.",
  provenance: "Synthetic Work observation",
  author_id: "owner",
  state: "pending-local",
  verification: "unverified",
  associations: { topics: ["build"], identifiers: ["BUILD-42"] },
};
const proposal = {
  id: "proposal-1",
  revision_id: "proposal-revision-1",
  state: "approval-required",
  content: "A generalized lesson, without Work particulars.",
  sources: [{ artifact_id: "memory-work", revision_id: revision.id }],
  destination: { space_id: "personal", repo_id: "personal-repo" },
};
const review = {
  proposal,
  source_context: context,
  destination_context: {
    ...context,
    space_id: "personal",
    space_name: "Personal",
    repo_id: "personal-repo",
    checkout: "/synthetic/personal",
  },
  sources: [{ artifact_id: "memory-work", kind: "memory", revision }],
  policies: [
    { scope: "space", id: "work", action: "export", allowed: true, epoch: 1 },
    {
      scope: "repository",
      id: "personal-repo",
      action: "publication",
      allowed: true,
      epoch: 1,
    },
  ],
  audience: "local owner",
  placement: "private local state",
  provenance: "Human-reviewed generalized lesson",
  dependencies: [],
  snapshot: "exact-review-snapshot",
};

async function scan(page: Page) {
  const result = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(result.violations).toEqual([]);
}

test("draft retries reuse operation identity and clearing context discards an in-flight save", async ({
  page,
}) => {
  await page.route("**/api/context", (route) =>
    route.fulfill({ json: context }),
  );
  await page.route("**/api/artifacts", (route) =>
    route.fulfill({
      json: {
        context,
        artifacts: [
          {
            id: "memory-work",
            kind: "memory",
            lifecycle: "active",
            head_revision: revision.id,
          },
        ],
        proposals: [],
      },
    }),
  );
  await page.route("**/api/artifacts/memory-work", (route) =>
    route.fulfill({
      json: {
        artifact: {
          id: "memory-work",
          kind: "memory",
          lifecycle: "active",
          head_revision: revision.id,
          space_id: "work",
          repo_id: "work-repo",
          revisions: [revision],
        },
        choices: [],
        actions: [
          {
            action: "publication",
            state: "approval-required",
            reason: "Exact content requires human review.",
          },
        ],
      },
    }),
  );
  let releaseFirst: () => void = () => {};
  const firstGate = new Promise<void>((resolve) => {
    releaseFirst = resolve;
  });
  let releaseChanged: () => void = () => {};
  const changedGate = new Promise<void>((resolve) => {
    releaseChanged = resolve;
  });
  const operations: string[] = [];
  await page.route("**/api/publications", async (route) => {
    operations.push(route.request().postDataJSON().operation_id);
    if (operations.length === 1) {
      await firstGate;
      await route.abort("failed");
    } else if (operations.length === 2) {
      await route.fulfill({ json: proposal });
    } else {
      await changedGate;
      await route
        .fulfill({
          json: { ...proposal, content: "Changed generalized lesson." },
        })
        .catch(() => {});
    }
  });
  await page.goto("/");
  await page.getByLabel("Session credential", { exact: true }).fill("scope");
  await page
    .getByRole("button", { name: "Inspect context", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Inspect memory memory-work", exact: true })
    .click();
  await page
    .getByLabel("Publication text", { exact: true })
    .fill(proposal.content);
  await page.getByLabel("Personal space ID", { exact: true }).fill("personal");
  await page
    .getByLabel("Personal repository ID", { exact: true })
    .fill("personal-repo");
  const save = page.getByRole("button", {
    name: "Save publication draft",
    exact: true,
  });
  await save.click();
  await expect(page.getByTestId("draft-status")).toContainText(
    "Saving publication draft",
  );
  await save.focus();
  await page.keyboard.press("Enter");
  expect(operations).toHaveLength(1);
  releaseFirst();
  await expect(page.getByTestId("draft-status")).toContainText("Unavailable");
  await save.click();
  await expect(page.getByTestId("draft-status")).toContainText(
    "Pending human review",
  );
  expect(operations).toHaveLength(2);
  expect(operations[0]).toBe(operations[1]);
  await page
    .getByLabel("Publication text", { exact: true })
    .fill("Changed generalized lesson.");
  await save.click();
  await expect(page.getByTestId("draft-status")).toContainText(
    "Saving publication draft",
  );
  expect(operations).toHaveLength(3);
  expect(operations[2]).not.toBe(operations[1]);
  await page
    .getByRole("button", { name: "Clear context", exact: true })
    .click();
  releaseChanged();
  await expect(
    page.getByRole("heading", { name: "Artifacts in this context" }),
  ).toHaveCount(0);
  await expect(page.getByText(revision.content, { exact: true })).toHaveCount(
    0,
  );
  await expect(page.getByTestId("draft-status")).toHaveCount(0);
  await expect(
    page.getByLabel("Session credential", { exact: true }),
  ).toBeFocused();
});

test("inspection covers memories, skills, definitions, dependencies and unavailable or empty states", async ({
  page,
}) => {
  await page.route("**/api/context", (route) =>
    route.fulfill({ json: context }),
  );
  const artifacts = [
    {
      id: "memory",
      kind: "memory",
      lifecycle: "active",
      head_revision: "memory-head",
    },
    {
      id: "skill",
      kind: "skill",
      lifecycle: "active",
      head_revision: "skill-head",
    },
    {
      id: "definition",
      kind: "agent-definition",
      lifecycle: "active",
      head_revision: "",
    },
  ];
  let inventoryState = "populated";
  await page.route("**/api/artifacts", async (route) => {
    if (inventoryState === "unavailable") {
      await route.fulfill({
        status: 503,
        json: { code: "unavailable", error: "Start the local node." },
      });
    } else {
      await route.fulfill({
        json: {
          context,
          artifacts: inventoryState === "empty" ? [] : artifacts,
          proposals: [],
        },
      });
    }
  });
  await page.route("**/api/artifacts/*", async (route) => {
    const id = route.request().url().split("/").at(-1);
    const artifact = artifacts.find((artifact) => artifact.id === id);
    if (!artifact) throw new Error("Unexpected artifact request");
    await route.fulfill({
      json: {
        artifact: {
          ...artifact,
          space_id: "work",
          repo_id: "work-repo",
          revisions: [
            {
              ...revision,
              id: artifact.head_revision || "definition-candidate",
              content: `Exact ${artifact.kind} body`,
              state:
                id === "definition"
                  ? "candidate"
                  : id === "skill"
                    ? "approved"
                    : "pending-local",
              ...(id === "memory"
                ? {}
                : {
                    source: {
                      commit: "exact-commit",
                      path: `${id}.md`,
                      blob: "exact-blob",
                      files: [
                        {
                          path: "reference.md",
                          blob: "reference-blob",
                          content: "Complete dependency bytes",
                        },
                      ],
                    },
                  }),
            },
          ],
        },
        choices:
          id === "skill"
            ? [
                {
                  artifact_id: id,
                  qualified: "work/repository/skill/source/build",
                  alias: "build",
                  state: "conflict",
                  reason: "Alias has incomparable approved choices.",
                  revision_id: "skill-head",
                  overridable: false,
                },
              ]
            : [],
        actions: [
          {
            action: "publication",
            state: "denied",
            reason: "Export policy denies this source.",
          },
          {
            action: "native-activation",
            state: "unavailable",
            reason: "Native activation is unavailable.",
          },
        ],
      },
    });
  });
  await page.goto("/");
  await page.getByLabel("Session credential", { exact: true }).fill("scope");
  await page
    .getByRole("button", { name: "Inspect context", exact: true })
    .click();
  for (const artifact of artifacts) {
    await page
      .getByRole("button", {
        name: `Inspect ${artifact.kind} ${artifact.id}`,
        exact: true,
      })
      .click();
    await expect(
      page.getByText(`Exact ${artifact.kind} body`, { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Artifact details", exact: true }),
    ).toBeFocused();
    if (artifact.kind !== "memory")
      await expect(
        page.getByText("Complete dependency bytes", { exact: true }),
      ).toBeVisible();
    await expect(
      page.getByText("Export policy denies this source.", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByLabel("Publication text", { exact: true }),
    ).toHaveCount(0);
    await scan(page);
  }
  inventoryState = "unavailable";
  await page
    .getByRole("button", { name: "Refresh artifacts", exact: true })
    .click();
  await expect(page.getByTestId("artifact-status")).toContainText(
    "Unavailable: Start the local node.",
  );
  await expect(
    page.getByText("Complete dependency bytes", { exact: true }),
  ).toHaveCount(0);
  await scan(page);
  inventoryState = "empty";
  await page
    .getByRole("button", { name: "Refresh artifacts", exact: true })
    .click();
  await expect(page.getByTestId("artifact-status")).toContainText(
    "Empty: no artifacts",
  );
  await scan(page);
  await page.setViewportSize({ width: 320, height: 256 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("clear and credential changes cancel loading and cannot reveal late Work or review responses", async ({
  page,
}) => {
  let releaseContext: () => void = () => {};
  const contextGate = new Promise<void>((resolve) => {
    releaseContext = resolve;
  });
  await page.route("**/api/context", async (route) => {
    await contextGate;
    await route.fulfill({ json: context }).catch(() => {});
  });
  let releaseReview: () => void = () => {};
  const reviewGate = new Promise<void>((resolve) => {
    releaseReview = resolve;
  });
  await page.route("**/api/publication-review", async (route) => {
    await reviewGate;
    await route.fulfill({ json: review }).catch(() => {});
  });
  await page.goto("/");
  const credential = page.getByLabel("Session credential", { exact: true });
  await credential.fill("old-work-session");
  await page
    .getByRole("button", { name: "Inspect context", exact: true })
    .click();
  await expect(page.getByTestId("context-status")).toContainText(
    "Checking session",
  );
  await scan(page);
  await credential.fill("new-personal-session");
  releaseContext();
  await expect(page.getByTestId("context-status")).toContainText(
    "No session bound",
  );
  await expect(page.getByText(context.checkout, { exact: true })).toHaveCount(
    0,
  );
  await expect(
    page.getByRole("heading", { name: "Artifacts in this context" }),
  ).toHaveCount(0);
  const human = page.getByLabel("Human review credential", { exact: true });
  await human.fill("human-review");
  await page
    .getByRole("button", { name: "Load exact review", exact: true })
    .click();
  await expect(page.getByTestId("review-status")).toContainText(
    "Loading exact",
  );
  await scan(page);
  await page
    .getByRole("button", { name: "Clear human review", exact: true })
    .click();
  releaseReview();
  await expect(human).toHaveValue("");
  await expect(human).toBeFocused();
  await expect(page.getByTestId("review-status")).toContainText(
    "Human review cleared",
  );
  await expect(page.getByTestId("review-output")).toHaveCount(0);
  expect(
    await page.evaluate(() => ({
      local: localStorage.length,
      session: sessionStorage.length,
      cookie: document.cookie,
      url: location.search,
    })),
  ).toEqual({ local: 0, session: 0, cookie: "", url: "" });
});

test("human review retries reuse operation identity and stale snapshots reset confirmation", async ({
  page,
}) => {
  let activeReview = review;
  let releaseFirst: () => void = () => {};
  const firstGate = new Promise<void>((resolve) => {
    releaseFirst = resolve;
  });
  const operations: string[] = [];
  await page.route("**/api/publication-review", async (route) => {
    if (route.request().method() === "GET") {
      await route.fulfill({ json: activeReview });
      return;
    }
    operations.push(route.request().postDataJSON().operation_id);
    if (operations.length === 1) {
      await firstGate;
      await route.abort("failed");
    } else {
      await route.fulfill({
        status: 409,
        json: { code: "conflict", error: "Snapshot changed. Reload review." },
      });
    }
  });
  await page.goto("/");
  await page
    .getByLabel("Human review credential", { exact: true })
    .fill("human-review");
  const load = page.getByRole("button", {
    name: "Load exact review",
    exact: true,
  });
  await load.click();
  const confirmation = page.getByLabel(
    "I reviewed the exact output, destination, sources, dependencies, and policies.",
    { exact: true },
  );
  await confirmation.check();
  const publish = page.getByRole("button", {
    name: "Publish reviewed memory",
    exact: true,
  });
  await publish.click();
  await expect(page.getByTestId("review-status")).toContainText(
    "Submitting exact-content",
  );
  await publish.focus();
  await page.keyboard.press("Enter");
  expect(operations).toHaveLength(1);
  releaseFirst();
  await expect(page.getByTestId("review-status")).toContainText("Unavailable");
  await load.click();
  await expect(confirmation).not.toBeChecked();
  await confirmation.check();
  await publish.click();
  await expect(page.getByTestId("review-status")).toContainText(
    "Conflict: Snapshot changed",
  );
  expect(operations).toHaveLength(2);
  expect(operations[0]).toBe(operations[1]);
  await expect(confirmation).not.toBeChecked();
  await scan(page);
  activeReview = {
    ...review,
    snapshot: "new-snapshot",
    proposal: {
      ...proposal,
      revision_id: "new-proposal-revision",
      content: "Changed output requiring another review.",
    },
  };
  await load.click();
  await expect(page.getByTestId("review-output")).toHaveText(
    activeReview.proposal.content,
  );
  await expect(confirmation).not.toBeChecked();
  await page.setViewportSize({ width: 320, height: 256 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("authorized inspection keeps source explicit and publication requires separate human review", async ({
  page,
}) => {
  await page.route("**/api/context", (route) =>
    route.fulfill({ json: context }),
  );
  await page.route("**/api/artifacts", (route) =>
    route.fulfill({
      json: {
        context,
        artifacts: [
          {
            id: "memory-work",
            kind: "memory",
            lifecycle: "active",
            head_revision: revision.id,
          },
        ],
        proposals: [],
      },
    }),
  );
  await page.route("**/api/artifacts/memory-work", (route) =>
    route.fulfill({
      json: {
        artifact: {
          id: "memory-work",
          space_id: "work",
          repo_id: "work-repo",
          kind: "memory",
          lifecycle: "active",
          head_revision: revision.id,
          revisions: [
            revision,
            {
              ...revision,
              id: "competing-memory",
              content: "Preserved competing evidence",
              state: "conflict",
            },
          ],
        },
        choices: [],
        actions: [
          {
            action: "publication",
            state: "approval-required",
            reason: "Exact content requires human review.",
          },
          {
            action: "native-activation",
            state: "denied",
            reason: "Memory is evidence, not executable source.",
          },
        ],
      },
    }),
  );
  let drafts = 0;
  await page.route("**/api/publications", async (route) => {
    drafts++;
    expect(route.request().headers().authorization).toBe(
      "Bearer scoped-session",
    );
    expect(route.request().postDataJSON()).toMatchObject({
      content: proposal.content,
      sources: proposal.sources,
      destination: proposal.destination,
    });
    await route.fulfill({ json: proposal });
  });
  let publications = 0;
  await page.route("**/api/publication-review", async (route) => {
    if (route.request().headers().authorization !== "Bearer human-review") {
      await route.fulfill({
        status: 403,
        json: {
          code: "denied",
          error: "Dedicated human review credential required.",
        },
      });
      return;
    }
    if (route.request().method() === "GET") {
      await route.fulfill({ json: review });
      return;
    }
    publications++;
    expect(route.request().postDataJSON()).toMatchObject({
      revision_id: proposal.revision_id,
      snapshot: review.snapshot,
    });
    await route.fulfill({
      json: {
        operation_id: "publish",
        artifact_id: "personal-memory",
        revision_id: "published-revision",
        state: "published",
      },
    });
  });
  await page.goto("/");
  await page
    .getByLabel("Session credential", { exact: true })
    .fill("scoped-session");
  await page
    .getByRole("button", { name: "Inspect context", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Artifacts in this context" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Inspect memory memory-work", exact: true })
    .click();
  await expect(page.getByText(revision.content, { exact: true })).toBeVisible();
  await expect(
    page.getByText("Preserved competing evidence", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Memory is evidence, not executable source.", {
      exact: true,
    }),
  ).toBeVisible();
  await page
    .getByLabel("Publication text", { exact: true })
    .fill(proposal.content);
  await page.getByLabel("Personal space ID", { exact: true }).fill("personal");
  await page
    .getByLabel("Personal repository ID", { exact: true })
    .fill("personal-repo");
  await page
    .getByRole("button", { name: "Save publication draft", exact: true })
    .click();
  await expect(page.getByTestId("draft-status")).toContainText(
    "Pending human review",
  );
  expect(drafts).toBe(1);
  expect(publications).toBe(0);
  await scan(page);

  const human = page.getByLabel("Human review credential", { exact: true });
  await human.fill("scoped-session");
  await page
    .getByRole("button", { name: "Load exact review", exact: true })
    .click();
  await expect(page.getByTestId("review-status")).toContainText("Denied");
  await scan(page);
  await human.fill("human-review");
  await page
    .getByRole("button", { name: "Load exact review", exact: true })
    .click();
  const confirmation = page.getByLabel(
    "I reviewed the exact output, destination, sources, dependencies, and policies.",
    { exact: true },
  );
  await expect(confirmation).toBeVisible();
  await expect(page.getByTestId("review-output")).toHaveText(proposal.content);
  await expect(page.getByTestId("review-status")).toContainText(
    "Pending confirmation",
  );
  const reviewSection = page.getByRole("region", {
    name: "Human publication review",
    exact: true,
  });
  await reviewSection.screenshot({
    path: "test-results/publication-review-desktop.png",
  });
  await page.setViewportSize({ width: 320, height: 800 });
  await reviewSection.screenshot({
    path: "test-results/publication-review-mobile.png",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.setViewportSize({ width: 1280, height: 720 });
  await expect(
    page.getByRole("button", { name: "Publish reviewed memory", exact: true }),
  ).toHaveAttribute("aria-disabled", "true");
  await confirmation.check();
  await human.fill("replacement-credential");
  await expect(confirmation).toHaveCount(0);
  await expect(page.getByTestId("review-output")).toHaveCount(0);
  await human.fill("human-review");
  await page
    .getByRole("button", { name: "Load exact review", exact: true })
    .click();
  await expect(confirmation).not.toBeChecked();
  await confirmation.focus();
  await page.keyboard.press("Space");
  await page
    .getByRole("button", { name: "Publish reviewed memory", exact: true })
    .focus();
  await page.keyboard.press("Enter");
  await expect(page.getByTestId("review-status")).toContainText(
    "Published separate unverified memory",
  );
  expect(publications).toBe(1);
  await scan(page);
  await page.setViewportSize({ width: 320, height: 800 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page
    .getByRole("button", { name: "Clear context", exact: true })
    .click();
  await expect(page.getByText(revision.content, { exact: true })).toHaveCount(
    0,
  );
  await expect(page.getByTestId("review-output")).toHaveCount(0);
  await expect(
    page.getByLabel("Session credential", { exact: true }),
  ).toBeFocused();
});
