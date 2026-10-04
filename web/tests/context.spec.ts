import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test("session context stays explicit, rejects credentials, and clears by keyboard", async ({
  page,
}) => {
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Session context" }),
  ).toBeVisible();
  const credential = page.getByLabel("Session credential");
  await credential.fill("synthetic-invalid-credential");
  await credential.press("Enter");
  await expect(page.getByTestId("context-status")).toContainText(
    "Context denied",
  );
  let scan = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(scan.violations).toEqual([]);
  await page.route("**/api/context", async (route) => {
    await route.fulfill({
      json: {
        owner_id: "owner",
        space_id: "space",
        space_name: "Work",
        repo_id: "repo",
        checkout: "/synthetic/repos/project",
      },
    });
  });
  await credential.fill("synthetic-trusted-credential");
  await credential.press("Enter");
  await expect(page.getByTestId("context-status")).toContainText(
    "Verified Work",
  );
  await expect(
    page.getByText("/synthetic/repos/project", { exact: true }),
  ).toBeVisible();
  scan = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(scan.violations).toEqual([]);
  await page.setViewportSize({ width: 320, height: 800 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.getByRole("button", { name: "Clear context" }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByTestId("context-status")).toContainText(
    "No session bound",
  );
  await expect(
    page.getByText("/synthetic/repos/project", { exact: true }),
  ).toHaveCount(0);
  await expect(credential).toBeFocused();
  await page.route("**/api/context", async (route) =>
    route.fulfill({ status: 503, body: "Unavailable" }),
  );
  await credential.fill("synthetic-trusted-credential");
  await credential.press("Enter");
  await expect(page.getByTestId("context-status")).toContainText(
    "Local authority unavailable",
  );
});
