import AxeBuilder from "@axe-core/playwright";
import type { Page } from "@playwright/test";
import { expect, test } from "@playwright/test";

const workContext = {
  owner_id: "owner",
  space_id: "work",
  space_name: "Work",
  repo_id: "work-repo",
  checkout: "/synthetic/work",
};
const partialMaintenance = {
  mode: "lexical",
  paused: true,
  state: "failed",
  queued: 201,
  deferred: 3,
  failed: 2,
  coverage: { eligible: 250, indexed: 49, pending: 201, limited: 1 },
};

async function bind(page: Page, credential = "work") {
  await page.getByLabel("Session credential", { exact: true }).fill(credential);
  await page
    .getByRole("button", { name: "Inspect context", exact: true })
    .click();
}

async function scan(page: Page) {
  const result = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(result.violations).toEqual([]);
}

test("maintenance keeps scoped queue counts and incomplete coverage visible while the installation is paused", async ({
  page,
}) => {
  await page.route("**/api/context", (route) =>
    route.fulfill({ json: workContext }),
  );
  await page.route("**/api/artifacts", (route) =>
    route.fulfill({
      json: {
        context: workContext,
        artifacts: [],
        proposals: [],
        maintenance: partialMaintenance,
      },
    }),
  );
  await page.goto("/");
  await bind(page);
  const status = page.getByRole("region", {
    name: "Retrieval and maintenance",
  });
  await expect(status).toBeVisible();
  await expect(status).toContainText("Lexical");
  await expect(status).toContainText("Installation indexing pause");
  await expect(status).toContainText("Paused");
  await expect(status).toContainText("Failed");
  await expect(status.locator("dd")).toContainText([
    "Lexical",
    "Paused",
    "Failed",
    "201",
    "3",
    "2",
    "250",
    "49",
    "201",
    "1",
  ]);
  await expect(status).toContainText("Partial lexical coverage");
  await expect(status).toContainText("owner, space, and repository");
  await expect(status).toContainText("inventory limit");
  await expect(status.getByRole("button")).toHaveCount(0);
  await scan(page);
  await status.screenshot({ path: "test-results/maintenance-desktop.png" });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.setViewportSize({ width: 320, height: 256 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await status.screenshot({ path: "test-results/maintenance-mobile.png" });
});

test("refresh separates empty, pending, deferred, complete and limited lexical coverage from queue readiness", async ({
  page,
}) => {
  await page.route("**/api/context", (route) =>
    route.fulfill({ json: workContext }),
  );
  let maintenance = {
    ...partialMaintenance,
    paused: false,
    state: "ready",
    queued: 0,
    deferred: 0,
    failed: 0,
    coverage: { eligible: 4, indexed: 4, pending: 0, limited: 1 },
  };
  await page.route("**/api/artifacts", (route) =>
    route.fulfill({
      json: { context: workContext, artifacts: [], proposals: [], maintenance },
    }),
  );
  await page.goto("/");
  await bind(page);
  const status = page.getByRole("region", {
    name: "Retrieval and maintenance",
  });
  const refresh = page.getByRole("button", {
    name: "Refresh artifacts",
    exact: true,
  });
  await expect(status).toContainText("Ready");
  await expect(status).toContainText("Not paused");
  await expect(status).toContainText("Partial lexical coverage");
  await expect(status).not.toContainText("Complete lexical coverage");

  maintenance = {
    ...maintenance,
    coverage: { eligible: 4, indexed: 3, pending: 1, limited: 0 },
  };
  await refresh.click();
  await expect(status).toContainText("Ready");
  await expect(status).toContainText("Partial lexical coverage");
  await expect(status).not.toContainText("Complete lexical coverage");

  maintenance = {
    ...maintenance,
    coverage: { eligible: 4, indexed: 4, pending: 0, limited: 0 },
  };
  await refresh.focus();
  await page.keyboard.press("Enter");
  await expect(status).toContainText("Complete lexical coverage");
  await expect(refresh).toBeFocused();

  maintenance = {
    ...maintenance,
    coverage: { eligible: 0, indexed: 0, pending: 0, limited: 0 },
  };
  await page.keyboard.press("Enter");
  await expect(status).toContainText("Empty lexical coverage");
  await expect(status).toContainText("No eligible current revisions");
  await expect(status).not.toContainText("Complete lexical coverage");

  maintenance = {
    ...maintenance,
    state: "pending",
    queued: 2,
    coverage: { eligible: 2, indexed: 0, pending: 2, limited: 0 },
  };
  await page.keyboard.press("Enter");
  await expect(status).toContainText("Pending");
  await expect(status).toContainText("Empty lexical coverage");
  await expect(status).toContainText("No current revisions indexed yet");

  maintenance = {
    ...maintenance,
    state: "deferred",
    queued: 0,
    deferred: 2,
    coverage: { eligible: 4, indexed: 2, pending: 2, limited: 0 },
  };
  await page.keyboard.press("Enter");
  await expect(status).toContainText("Deferred");
  await expect(status).toContainText("Partial lexical coverage");
  await scan(page);
});

test("missing and malformed maintenance remains explicitly unavailable without hiding permitted artifacts", async ({
  page,
}) => {
  await page.route("**/api/context", (route) =>
    route.fulfill({ json: workContext }),
  );
  let maintenance: unknown;
  await page.route("**/api/artifacts", (route) =>
    route.fulfill({
      json: {
        context: workContext,
        artifacts: [
          {
            id: "permitted",
            kind: "memory",
            lifecycle: "active",
            head_revision: "current",
          },
        ],
        proposals: [],
        maintenance,
      },
    }),
  );
  await page.goto("/");
  await bind(page);
  const status = page.getByRole("region", {
    name: "Retrieval and maintenance",
  });
  const invalidStatuses = [
    undefined,
    null,
    {},
    { ...partialMaintenance, mode: "neural" },
    { ...partialMaintenance, paused: "true" },
    { ...partialMaintenance, state: "active" },
    { ...partialMaintenance, queued: -1 },
    { ...partialMaintenance, deferred: 0.5 },
    { ...partialMaintenance, failed: "2" },
    { ...partialMaintenance, coverage: null },
    {
      ...partialMaintenance,
      coverage: { eligible: 1, indexed: null, pending: 1, limited: 0 },
    },
    {
      ...partialMaintenance,
      coverage: { eligible: 1, indexed: 0, pending: -1, limited: 0 },
    },
    {
      ...partialMaintenance,
      coverage: { eligible: 1, indexed: 2, pending: 0, limited: 0 },
    },
    {
      ...partialMaintenance,
      coverage: { eligible: 2, indexed: 1, pending: 0, limited: 0 },
    },
    {
      ...partialMaintenance,
      coverage: { eligible: 2, indexed: 1, pending: 1, limited: 2 },
    },
    { ...partialMaintenance, state: "ready" },
    { ...partialMaintenance, state: "pending" },
    { ...partialMaintenance, failed: 0 },
  ];
  for (const invalid of invalidStatuses) {
    maintenance = invalid;
    await page
      .getByRole("button", { name: "Refresh artifacts", exact: true })
      .click();
    await expect(status).toContainText("Unavailable");
    await expect(status).toContainText("Refresh artifacts");
    await expect(status).not.toContainText("Ready");
    await expect(status).not.toContainText("Complete lexical coverage");
    await expect(
      page.getByRole("button", {
        name: "Inspect memory permitted",
        exact: true,
      }),
    ).toBeVisible();
  }
  await scan(page);
});

test("refresh discards protected maintenance when current authority denies access", async ({
  page,
}) => {
  await page.route("**/api/context", (route) =>
    route.fulfill({ json: workContext }),
  );
  await page.route("**/api/artifacts", (route) =>
    route.fulfill({
      json: {
        context: workContext,
        artifacts: [],
        proposals: [],
        maintenance: partialMaintenance,
      },
    }),
  );
  await page.goto("/");
  await bind(page);
  const status = page.getByRole("region", {
    name: "Retrieval and maintenance",
  });
  await expect(status).toContainText("201");
  await page.route("**/api/artifacts", (route) =>
    route.fulfill({
      status: 403,
      json: { error: "Current session is denied." },
    }),
  );
  await page
    .getByRole("button", { name: "Refresh artifacts", exact: true })
    .click();
  await expect(page.getByTestId("artifact-status")).toContainText("Denied");
  await expect(status).toHaveCount(0);
  await scan(page);
});

test("clearing an in-flight Work refresh prevents late maintenance from entering a Personal session", async ({
  page,
}) => {
  const personalContext = {
    ...workContext,
    space_id: "personal",
    space_name: "Personal",
    repo_id: "personal-repo",
    checkout: "/synthetic/personal",
  };
  await page.route("**/api/context", (route) =>
    route.fulfill({
      json:
        route.request().headers().authorization === "Bearer personal"
          ? personalContext
          : workContext,
    }),
  );
  let releaseWork: () => void = () => {};
  const gate = new Promise<void>((resolve) => {
    releaseWork = resolve;
  });
  let holdWork = false;
  let completeWork: () => void = () => {};
  const workCompleted = new Promise<void>((resolve) => {
    completeWork = resolve;
  });
  await page.route("**/api/artifacts", async (route) => {
    if (route.request().headers().authorization === "Bearer personal") {
      await route.fulfill({
        json: {
          context: personalContext,
          artifacts: [],
          proposals: [],
          maintenance: {
            ...partialMaintenance,
            state: "ready",
            queued: 0,
            deferred: 0,
            failed: 0,
            coverage: { eligible: 0, indexed: 0, pending: 0, limited: 0 },
          },
        },
      });
      return;
    }
    if (holdWork) await gate;
    await route
      .fulfill({
        json: {
          context: workContext,
          artifacts: [],
          proposals: [],
          maintenance: {
            ...partialMaintenance,
            active_job: "work-private-job",
            active: true,
          },
        },
      })
      .catch(() => {});
    if (holdWork) completeWork();
  });
  await page.goto("/");
  await bind(page);
  const status = page.getByRole("region", {
    name: "Retrieval and maintenance",
  });
  await expect(status).toContainText("201");
  await expect(page.getByText("work-private-job")).toHaveCount(0);
  holdWork = true;
  await page
    .getByRole("button", { name: "Refresh artifacts", exact: true })
    .click();
  await expect(page.getByTestId("artifact-status")).toContainText("Loading");
  const cancelled = page.waitForEvent("requestfailed", {
    predicate: (request) => request.url().endsWith("/api/artifacts"),
  });
  await page
    .getByRole("button", { name: "Clear context", exact: true })
    .focus();
  await page.keyboard.press("Enter");
  await expect(status).toHaveCount(0);
  await expect(
    page.getByLabel("Session credential", { exact: true }),
  ).toBeFocused();
  await bind(page, "personal");
  await expect(status).toContainText("Empty lexical coverage");
  releaseWork();
  await workCompleted;
  await cancelled;
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
  );
  await expect(page.getByTestId("context-status")).toContainText(
    "Verified Personal",
  );
  await expect(status).not.toContainText("201");
  await expect(status).not.toContainText("Failed");
  await expect(page.getByText("work-private-job")).toHaveCount(0);
  await scan(page);
});
