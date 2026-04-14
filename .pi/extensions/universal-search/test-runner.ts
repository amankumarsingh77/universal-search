import { execSync, spawn } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { updateTestState } from "./tui.js";

export interface TestResult {
  layer: "headless" | "e2e";
  passed: boolean;
  passCount: number;
  failCount: number;
  output: string;
}

export function registerTestRunner(pi: ExtensionAPI): void {
  pi.registerCommand("us-test", {
    description: "Run headless Go tests + Playwright e2e smoke tests",
    handler: async (_args, ctx) => {
      await ctx.waitForIdle();
      const result = await runAllTests(ctx.cwd, ctx);
      if (result.every((r) => r.passed)) {
        ctx.ui.notify("All tests passed", "success");
      } else {
        ctx.ui.notify("Tests failed — check widget for details", "error");
      }
    },
  });
}

export async function runAllTests(
  worktreePath: string,
  ctx: { ui: any },
): Promise<TestResult[]> {
  // Layer 1: headless Go tests
  updateTestState({ headless: "running" }, ctx);
  const headlessResult = await runHeadlessTests(worktreePath);
  updateTestState(
    {
      headless: headlessResult.passed ? "passed" : "failed",
      headlessCount: { pass: headlessResult.passCount, fail: headlessResult.failCount },
    },
    ctx,
  );

  if (!headlessResult.passed) {
    // Skip e2e when headless fails — fail fast
    return [headlessResult];
  }

  // Layer 2: e2e
  updateTestState({ e2e: "running" }, ctx);
  const e2eResult = await runE2ETests(worktreePath);
  updateTestState({ e2e: e2eResult.passed ? "passed" : "failed" }, ctx);

  return [headlessResult, e2eResult];
}

async function runHeadlessTests(worktreePath: string): Promise<TestResult> {
  return new Promise((resolve) => {
    let output = "";
    const proc = spawn("go", ["test", "-race", "-count=1", "./..."], {
      cwd: worktreePath,
      stdio: ["ignore", "pipe", "pipe"],
    });

    proc.stdout.on("data", (d: Buffer) => { output += d.toString(); });
    proc.stderr.on("data", (d: Buffer) => { output += d.toString(); });

    proc.on("close", (code: number | null) => {
      const passMatches = output.match(/^ok\s+/gm) ?? [];
      const failMatches = output.match(/^FAIL\s+/gm) ?? [];
      resolve({
        layer: "headless",
        passed: code === 0,
        passCount: passMatches.length,
        failCount: failMatches.length,
        output,
      });
    });
  });
}

async function runE2ETests(worktreePath: string): Promise<TestResult> {
  let output = "";

  try {
    output += "Building Wails binary...\n";
    execSync("wails build -tags webkit2_41", {
      cwd: worktreePath,
      stdio: "pipe",
      timeout: 300_000,
    });
    output += "Build succeeded.\n";
  } catch (err: any) {
    output += `Build failed: ${err.message}\n${err.stderr ?? ""}`;
    return { layer: "e2e", passed: false, passCount: 0, failCount: 1, output };
  }

  return new Promise((resolve) => {
    const e2eDir = path.join(worktreePath, "e2e");
    if (!fs.existsSync(e2eDir)) {
      resolve({
        layer: "e2e",
        passed: false,
        passCount: 0,
        failCount: 1,
        output: "e2e/ directory not found",
      });
      return;
    }

    const proc = spawn("npx", ["playwright", "test", "--reporter=list"], {
      cwd: e2eDir,
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env, US_E2E_MODE: "1" },
    });

    proc.stdout.on("data", (d: Buffer) => { output += d.toString(); });
    proc.stderr.on("data", (d: Buffer) => { output += d.toString(); });

    proc.on("close", (code: number | null) => {
      const passMatches = output.match(/✓/g) ?? [];
      const failMatches = output.match(/✗|failed/gi) ?? [];
      resolve({
        layer: "e2e",
        passed: code === 0,
        passCount: passMatches.length,
        failCount: failMatches.length,
        output,
      });
    });
  });
}

export function captureTestOutput(result: TestResult): string {
  return result.output;
}
