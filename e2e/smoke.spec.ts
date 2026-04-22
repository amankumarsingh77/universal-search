import { test, expect } from "@playwright/test";
import { spawn } from "node:child_process";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

function getBinaryPath(): string {
  const candidates = [
    path.join(__dirname, "..", "build", "bin", "findo"),
    path.join(__dirname, "..", "build", "bin", "findo.app", "Contents", "MacOS", "findo"),
  ];
  for (const c of candidates) {
    if (fs.existsSync(c)) return c;
  }
  throw new Error("Built binary not found. Run wails build first.");
}

let appProcess: ReturnType<typeof spawn> | null = null;
let tempDataDir: string;
let tempIndexDir: string;

test.beforeAll(async () => {
  tempDataDir = fs.mkdtempSync(path.join(os.tmpdir(), "us-e2e-data-"));
  tempIndexDir = fs.mkdtempSync(path.join(os.tmpdir(), "us-e2e-index-"));

  const files = [
    { name: "report.txt", content: "quarterly sales report results summary" },
    { name: "main.go", content: "package main\n\nfunc main() {}" },
    { name: "notes.md", content: "# Meeting notes\n\nAction items for next sprint" },
    { name: "data.json", content: '{"key": "value", "items": [1, 2, 3]}' },
    { name: "readme.txt", content: "Installation instructions for findo" },
    { name: "budget.txt", content: "annual budget planning spreadsheet data" },
    { name: "design.md", content: "# Design spec\n\nArchitecture overview" },
    { name: "config.go", content: "package config\n\ntype Config struct{}" },
    { name: "todo.txt", content: "buy groceries call dentist finish project" },
    { name: "log.txt", content: "error: connection refused at line 42" },
  ];
  for (const f of files) {
    fs.writeFileSync(path.join(tempIndexDir, f.name), f.content);
  }

  const binaryPath = getBinaryPath();
  appProcess = spawn(binaryPath, [], {
    env: { ...process.env, US_E2E_MODE: "1", US_DATA_DIR: tempDataDir },
    stdio: "ignore",
  });

  await new Promise((r) => setTimeout(r, 3000));
});

test.afterAll(async () => {
  appProcess?.kill();
  fs.rmSync(tempDataDir, { recursive: true, force: true });
  fs.rmSync(tempIndexDir, { recursive: true, force: true });
});

test("app launches and search window is accessible", async ({ page }) => {
  await page.goto("http://localhost:34115");
  await expect(page.locator('[data-testid="search-input"]')).toBeVisible({ timeout: 10_000 });
});

test("index a directory and wait for completion", async ({ page }) => {
  await page.goto("http://localhost:34115");
  await page.locator('[data-testid="add-folder-btn"]').click();
  await page.evaluate((dir) => {
    return (window as any).go.main.App.AddFolder(dir);
  }, tempIndexDir);
  await page.waitForFunction(
    async () => {
      const status = await (window as any).go.main.App.GetIndexStatus();
      return status.IsComplete === true;
    },
    { timeout: 30_000 },
  );
});

test("search returns expected file for known query", async ({ page }) => {
  await page.goto("http://localhost:34115");
  const input = page.locator('[data-testid="search-input"]');
  await input.fill("quarterly report");
  await input.press("Enter");
  await expect(page.locator('[data-testid="result-item"]').filter({ hasText: "report.txt" })).toBeVisible({
    timeout: 5_000,
  });
});

test("filter chips render for structured query", async ({ page }) => {
  await page.goto("http://localhost:34115");
  const input = page.locator('[data-testid="search-input"]');
  await input.fill("ext:go main function");
  await input.press("Enter");
  await expect(page.locator('[data-testid="filter-chip"]').filter({ hasText: "ext:go" })).toBeVisible({
    timeout: 5_000,
  });
});

test("preview panel loads for text file", async ({ page }) => {
  await page.goto("http://localhost:34115");
  const input = page.locator('[data-testid="search-input"]');
  await input.fill("installation instructions");
  await input.press("Enter");
  await page.locator('[data-testid="result-item"]').filter({ hasText: "readme.txt" }).click();
  await expect(page.locator('[data-testid="preview-panel"]')).toBeVisible({ timeout: 3_000 });
  await expect(page.locator('[data-testid="preview-panel"]')).toContainText("Installation");
});

// ---------------------------------------------------------------------------
// Failures modal smoke test
//
// This test requires a locally built Wails binary and exercises the
// indexing-failure-visibility feature end-to-end. It does NOT run by default
// in CI because it needs the built app binary and takes 30+ seconds.
//
// To run locally:
//   E2E_FAILURES_MODAL=1 npx playwright test e2e/smoke.spec.ts --grep "failures modal"
// ---------------------------------------------------------------------------
const runFailuresModal = process.env["E2E_FAILURES_MODAL"] === "1";

// ---------------------------------------------------------------------------
// Error banner + warning chip smoke tests  (Phase 6 — REQ-018, REQ-020)
//
// These tests require a locally built Wails binary.  They exercise the
// per-search error banner (ErrorBanner.tsx) and the query-parse-timeout
// warning chip (WarningChip.tsx) by overriding the Wails JS bindings in the
// browser context, so the backend is never actually called for these flows.
//
// Limitation: the smoke harness spawns a real binary and navigates to its
// built-in HTTP server.  Per-test mock injection is done via page.evaluate()
// to monkey-patch window.go.main.App methods AFTER the page loads, which is
// the only seam available without rebuilding the app.  This approach works
// reliably for render-only assertions (banner text, button disabled state).
//
// To run locally:
//   E2E_ERROR_BANNER=1 npx playwright test e2e/smoke.spec.ts --grep "error banner"
// ---------------------------------------------------------------------------
const runErrorBanner = process.env["E2E_ERROR_BANNER"] === "1";

test.describe.configure({ mode: "serial" });

(runFailuresModal ? test.describe : test.describe.skip)(
  "failures modal — requires local binary (E2E_FAILURES_MODAL=1)",
  () => {
    let failTempDir: string;

    test.beforeAll(async () => {
      // Create a dedicated temp directory with a file that has an unsupported
      // extension (.xyz). The chunker registry has no handler for .xyz so the
      // pipeline records an ERR_UNSUPPORTED_FORMAT terminal failure.
      failTempDir = fs.mkdtempSync(path.join(os.tmpdir(), "us-e2e-fail-"));
      fs.writeFileSync(
        path.join(failTempDir, "unsupported.xyz"),
        "this file type is not in the chunker registry",
      );
    });

    test.afterAll(async () => {
      fs.rmSync(failTempDir, { recursive: true, force: true });
    });

    test("failures modal shows View button and lists the failed file", async ({ page }) => {
      await page.goto("http://localhost:34115");

      // Add the folder containing the unsupported file so the pipeline indexes it.
      await page.evaluate((dir) => {
        return (window as any).go.main.App.AddFolder(dir);
      }, failTempDir);

      // Wait until at least one failure is recorded (up to 60 s).
      await page.waitForFunction(
        async () => {
          const status = await (window as any).go.main.App.GetIndexStatus();
          return (status.FailedFiles ?? status.failedFiles ?? 0) > 0;
        },
        { timeout: 60_000 },
      );

      // The "View" button appears inside the IndexingBar whenever failedFiles > 0.
      const viewButton = page.getByRole("button", { name: "View" });
      await expect(viewButton).toBeVisible({ timeout: 5_000 });

      // Click "View" — the FailuresModal should open.
      await viewButton.click();

      // Modal is identified by its aria-label.
      const modal = page.getByRole("dialog", { name: "Indexing failures" });
      await expect(modal).toBeVisible({ timeout: 5_000 });

      // At least one group row should show the "Unsupported format" label.
      const groupLabel = modal.getByText("Unsupported format");
      await expect(groupLabel).toBeVisible({ timeout: 5_000 });

      // Expand the group by clicking its header button.
      await modal.getByRole("button", { name: /Unsupported format/ }).click();

      // After expanding, the failed filename should be visible in the detail rows.
      await expect(modal.getByText("unsupported.xyz")).toBeVisible({ timeout: 5_000 });

      // Close the modal via the footer "Close" button.
      await modal.getByRole("button", { name: "Close" }).click();
      await expect(modal).not.toBeVisible({ timeout: 3_000 });
    });
  },
);

(runErrorBanner ? test.describe : test.describe.skip)(
  "error banner + warning chip — requires local binary (E2E_ERROR_BANNER=1)",
  () => {
    // -----------------------------------------------------------------------
    // Scenario A: ERR_EMBED_FAILED banner visibility
    //
    // Monkey-patch window.go.main.App.SearchWithFilters to return an error
    // response carrying errorCode="ERR_EMBED_FAILED".  The React app should
    // render the ErrorBanner with the human-readable label from CODE_LABELS.
    // -----------------------------------------------------------------------
    test("ERR_EMBED_FAILED — error banner shows 'Embedding failed' label", async ({ page }) => {
      await page.goto("http://localhost:34115");

      // Wait for the search input to be ready.
      await expect(page.locator('[data-testid="search-input"]')).toBeVisible({ timeout: 10_000 });

      // Inject mock — override SearchWithFilters to simulate ERR_EMBED_FAILED.
      await page.evaluate(() => {
        const go = (window as any).go;
        if (!go?.main?.App) return;
        go.main.App.SearchWithFilters = async () => ({
          results: [],
          errorCode: "ERR_EMBED_FAILED",
          retryAfterMs: 0,
        });
      });

      // Type a query to trigger the search path.
      const input = page.locator('[data-testid="search-input"]');
      await input.fill("quarterly report");
      await input.press("Enter");

      // The ErrorBanner should appear with the label mapped from CODE_LABELS.
      const banner = page.locator('[data-testid="error-banner"]');
      await expect(banner).toBeVisible({ timeout: 5_000 });
      await expect(banner).toContainText("Embedding failed");
    });

    // -----------------------------------------------------------------------
    // Scenario B: ERR_RATE_LIMITED countdown + disabled Retry button
    //
    // Monkey-patch SearchWithFilters to return errorCode="ERR_RATE_LIMITED"
    // with retryAfterMs=5000.  The ErrorBanner should display "5s" in the
    // countdown span and the Retry button should be disabled initially.
    // -----------------------------------------------------------------------
    test("ERR_RATE_LIMITED — shows 5s countdown and Retry button is disabled", async ({ page }) => {
      await page.goto("http://localhost:34115");

      await expect(page.locator('[data-testid="search-input"]')).toBeVisible({ timeout: 10_000 });

      // Inject mock.
      await page.evaluate(() => {
        const go = (window as any).go;
        if (!go?.main?.App) return;
        go.main.App.SearchWithFilters = async () => ({
          results: [],
          errorCode: "ERR_RATE_LIMITED",
          retryAfterMs: 5000,
        });
      });

      const input = page.locator('[data-testid="search-input"]');
      await input.fill("budget planning");
      await input.press("Enter");

      const banner = page.locator('[data-testid="error-banner"]');
      await expect(banner).toBeVisible({ timeout: 5_000 });

      // Countdown span should show "5s" immediately after render.
      await expect(banner).toContainText("5s");

      // Retry button should be disabled while the countdown is active.
      const retryBtn = banner.getByRole("button", { name: "Retry" });
      await expect(retryBtn).toBeDisabled({ timeout: 3_000 });
    });

    // -----------------------------------------------------------------------
    // Scenario C: query_parse_timeout warning chip — search still runs
    //
    // Monkey-patch ParseQuery to return {warning: 'query_parse_timeout', ...}
    // and SearchWithFilters to return one fake result.  The WarningChip should
    // be visible in the chip row AND the fake result should appear, confirming
    // that a parse timeout does not block the search.
    // -----------------------------------------------------------------------
    test("query_parse_timeout — warning chip visible and result still renders", async ({ page }) => {
      await page.goto("http://localhost:34115");

      await expect(page.locator('[data-testid="search-input"]')).toBeVisible({ timeout: 10_000 });

      // Inject mocks — ParseQuery returns a timeout warning, SearchWithFilters
      // returns one stub result so we can assert it renders.
      await page.evaluate(() => {
        const go = (window as any).go;
        if (!go?.main?.App) return;
        go.main.App.ParseQuery = async () => ({
          warning: "query_parse_timeout",
          chips: [],
          semanticQuery: "hello",
          errorCode: "",
          retryAfterMs: 0,
        });
        go.main.App.SearchWithFilters = async () => ({
          results: [
            {
              fileId: 1,
              filePath: "/tmp/hello.txt",
              fileName: "hello.txt",
              fileType: "text",
              extension: ".txt",
              score: 0.9,
              snippet: "hello world",
              modifiedAt: new Date().toISOString(),
            },
          ],
          errorCode: "",
          retryAfterMs: 0,
        });
      });

      const input = page.locator('[data-testid="search-input"]');
      await input.fill("hello");
      // Trigger parse immediately via Enter.
      await input.press("Enter");

      // The WarningChip renders inside the chip row in SearchBar.tsx.
      // Its label text is "Query understanding was slow".
      await expect(page.getByText("Query understanding was slow")).toBeVisible({ timeout: 5_000 });

      // The search result should also render despite the parse timeout.
      await expect(page.locator('[data-testid="result-item"]').filter({ hasText: "hello.txt" })).toBeVisible({
        timeout: 5_000,
      });
    });
  },
);
