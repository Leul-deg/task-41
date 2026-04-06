const { test, expect } = require("@playwright/test");

const password = process.env.E2E_PASSWORD || "LocalAdminPass123!";

async function login(page, username) {
  await page.goto("/");
  await page.fill("#username", username);
  await page.fill("#password", password);
  await page.click("button[type='submit']");
  await expect(page).toHaveURL(/\/dashboard$/);
}

async function mockApplications(page) {
  await page.route("**/rpc/api/hiring/applications", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ applications: [] }),
    });
  });
}

test("hiring dropdown loads jobs successfully", async ({ page }) => {
  await login(page, "admin");
  await mockApplications(page);

  await page.route("**/rpc/api/hiring/jobs", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ jobs: [{ id: "job-1", code: "JOB-1", title: "Picker" }] }),
    });
  });

  await page.goto("/hiring");
  await expect(page.locator("#manual-job-id option").nth(1)).toHaveText("JOB-1 - Picker");
  await expect(page.locator("#manual-job-state")).toContainText("1 job(s) available");
});

test("hiring dropdown empty list state disables actions", async ({ page }) => {
  await login(page, "admin");
  await mockApplications(page);

  await page.route("**/rpc/api/hiring/jobs", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ jobs: [] }),
    });
  });

  await page.goto("/hiring");
  await expect(page.locator("#manual-job-id option").first()).toHaveText("No jobs available");
  await expect(page.locator("#csv-job-id option").first()).toHaveText("No jobs available");
  await expect(page.locator("#hiring-manual-submit")).toBeDisabled();
  await expect(page.locator("#csv-import-btn")).toBeDisabled();
});

test("hiring dropdown shows access denied on 403", async ({ page }) => {
  await login(page, "admin");
  await mockApplications(page);

  await page.route("**/rpc/api/hiring/jobs*", async (route) => {
    const url = route.request().url();
    if (url.includes("/for-intake")) {
      await route.fulfill({
        status: 403,
        contentType: "application/json",
        body: JSON.stringify({ error: "forbidden", code: "FORBIDDEN_SCOPE" }),
      });
      return;
    }
    await route.fulfill({
      status: 403,
      contentType: "application/json",
      body: JSON.stringify({ error: "forbidden", code: "FORBIDDEN_SCOPE" }),
    });
  });

  await page.goto("/hiring");
  await expect(page.locator("#manual-job-id option").first()).toHaveText("Access denied to job list");
  await expect(page.locator("#manual-job-state")).toContainText("Access denied to job list");
  await expect(page.locator("#hiring-manual-submit")).toBeDisabled();
});

test("hiring dropdown shows session-expired state on 401", async ({ page }) => {
  await login(page, "admin");
  await mockApplications(page);

  await page.route("**/rpc/api/hiring/jobs", async (route) => {
    await route.fulfill({
      status: 401,
      contentType: "application/json",
      body: JSON.stringify({ error: "unauthorized", code: "UNAUTHORIZED" }),
    });
  });

  await page.goto("/hiring");
  await expect(page.locator("#manual-job-state")).toContainText("Session expired while loading jobs");
  await expect(page.locator("#hiring-manual-submit")).toBeDisabled();
});

test("hiring dropdown retry recovers from transient failure", async ({ page }) => {
  await login(page, "admin");
  await mockApplications(page);

  let hit = 0;
  await page.route("**/rpc/api/hiring/jobs", async (route) => {
    hit += 1;
    if (hit === 1) {
      await route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({ error: "db temporary outage" }),
      });
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ jobs: [{ id: "job-2", code: "JOB-2", title: "Loader" }] }),
    });
  });

  await page.goto("/hiring");
  await expect(page.locator("#manual-job-id option").first()).toHaveText("Failed to load jobs (Retry)");
  await page.click("#manual-job-retry");
  await expect(page.locator("#manual-job-id option").nth(1)).toHaveText("JOB-2 - Loader");
  await expect(page.locator("#manual-job-state")).toContainText("1 job(s) available");
});
