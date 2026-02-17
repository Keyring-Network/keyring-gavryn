import { test, expect } from "@playwright/test";

test("landing shows control room", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByText("Gavryn", { exact: true })).toBeVisible();
  await expect(page.getByPlaceholder("Assign a task or ask anything")).toBeVisible();
});
