import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test("the packaged UI reports connection failure and recovers by keyboard", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page.getByRole("status")).toContainText("Connected");
  await page.route("**/api/status", (route) => route.abort());
  await page.keyboard.press("Tab");
  await expect(
    page.getByRole("button", { name: "Refresh status" }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("status")).toContainText("Unable to reach");
  await expect(
    page.getByRole("button", { name: "Refresh status" }),
  ).toBeFocused();
  const scan = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(scan.violations).toEqual([]);
  await page.unroute("**/api/status");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("status")).toContainText("Connected");
  await page.setViewportSize({ width: 320, height: 800 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});
