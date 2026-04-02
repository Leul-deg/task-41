const { test, expect } = require("@playwright/test");

const password = process.env.E2E_PASSWORD || "LocalAdminPass123!";

async function login(page, username) {
  await page.goto("/");
  await page.fill("#username", username);
  await page.fill("#password", password);
  await page.click("button[type='submit']");
}

test("unauthenticated /dashboard redirects to login", async ({ page }) => {
  await page.goto("/dashboard");
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator("#login-form")).toBeVisible();
});

test("admin login reaches and stays on dashboard", async ({ page }) => {
  await login(page, "admin");
  await expect(page).toHaveURL(/\/dashboard$/);
  await expect(page.locator("text=Operations Dashboard")).toBeVisible();

  await page.waitForTimeout(1200);
  await expect(page).toHaveURL(/\/dashboard$/);
});

test("recruiter blocked from compliance module with shell intact", async ({ page }) => {
  await login(page, "recruiter1");
  await expect(page).toHaveURL(/\/dashboard$/);

  await page.goto("/compliance");
  await expect(page).toHaveURL(/\/compliance$/);
  await expect(page.locator("text=Access Restricted")).toBeVisible();
  await expect(page.getByRole("link", { name: "Return to Dashboard" })).toBeVisible();
});
