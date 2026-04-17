const { test, expect } = require("@playwright/test");

const password = process.env.E2E_PASSWORD || "LocalAdminPass123!";

test("wrong password stays on login page", async ({ page }) => {
  await page.goto("/");
  await page.fill("#username", "admin");
  await page.fill("#password", "wrongpassword123!");
  await page.click("button[type='submit']");
  await page.waitForTimeout(1500);
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator("#login-form")).toBeVisible();
});

test("unauthenticated /support redirects to login", async ({ page }) => {
  await page.goto("/support");
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator("#login-form")).toBeVisible();
});

test("unauthenticated /inventory redirects to login", async ({ page }) => {
  await page.goto("/inventory");
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator("#login-form")).toBeVisible();
});

test("compliance1 can access compliance module", async ({ page }) => {
  await page.goto("/");
  await page.fill("#username", "compliance1");
  await page.fill("#password", password);
  await page.click("button[type='submit']");
  await expect(page).toHaveURL(/\/dashboard$/);

  await page.goto("/compliance");
  await expect(page).toHaveURL(/\/compliance$/);
  await expect(page.locator("text=Access Restricted")).not.toBeVisible();
});
