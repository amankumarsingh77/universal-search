import { spawn } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { runAllTests, captureTestOutput } from "./test-runner.js";
import { setWorkflowState, updatePhase, updateTestState, type PhaseStatus } from "./tui.js";

interface PhaseDefinition {
  name: string;
  description: string;
  files: string[];       // glob patterns this phase owns
  dependsOn: string[];   // phase names that must complete first
}

// Reads a ```phases JSON block from the spec markdown
function parsePhasesFromSpec(specContent: string): PhaseDefinition[] {
  const match = specContent.match(/```phases\n([\s\S]*?)```/);
  if (!match) return [];
  try {
    return JSON.parse(match[1]) as PhaseDefinition[];
  } catch {
    return [];
  }
}

function findLatestSpec(docsDir: string): string | null {
  const specsDir = path.join(docsDir, "superpowers", "specs");
  if (!fs.existsSync(specsDir)) return null;
  const files = fs.readdirSync(specsDir)
    .filter((f) => f.endsWith("-design.md"))
    .sort()
    .reverse();
  return files.length > 0 ? path.join(specsDir, files[0]) : null;
}

function globOverlaps(a: string[], b: string[]): boolean {
  for (const pa of a) {
    const prefixA = pa.replace(/\*.*$/, "");
    for (const pb of b) {
      const prefixB = pb.replace(/\*.*$/, "");
      if (prefixA.startsWith(prefixB) || prefixB.startsWith(prefixA)) return true;
    }
  }
  return false;
}

function getPiInvocation(args: string[]): { command: string; args: string[] } {
  const execName = path.basename(process.execPath).toLowerCase();
  const isGenericRuntime = /^(node|bun)(\.exe)?$/.test(execName);
  if (!isGenericRuntime) {
    return { command: process.execPath, args };
  }
  return { command: "pi", args };
}

async function spawnPhaseAgent(
  phase: PhaseDefinition,
  worktreePath: string,
  specExcerpt: string,
): Promise<{ success: boolean; finalMessage: string; output: string }> {
  return new Promise((resolve) => {
    const prompt = [
      `Implement phase: ${phase.name}`,
      `Description: ${phase.description}`,
      ``,
      `You own these files (do not modify files outside this list):`,
      phase.files.map((f) => `  - ${f}`).join("\n"),
      ``,
      `Spec context:`,
      specExcerpt,
      ``,
      `Rules:`,
      `- Pure Go, no CGO`,
      `- Follow existing patterns in this codebase`,
      `- Write tests alongside implementation`,
      `- Run go test ./... for your packages when done`,
      `- When complete, end your final message with exactly: DONE`,
      `- If you cannot proceed, end with: BLOCKED:<reason>`,
    ].join("\n");

    const args = ["--mode", "json", "-p", "--no-session", prompt];
    const invocation = getPiInvocation(args);

    const proc = spawn(invocation.command, invocation.args, {
      cwd: worktreePath,
      stdio: ["ignore", "pipe", "pipe"],
      shell: false,
    });

    let output = "";
    let finalMessage = "";
    let buffer = "";

    const processLine = (line: string) => {
      if (!line.trim()) return;
      let event: any;
      try { event = JSON.parse(line); } catch { return; }
      if (event.type === "message_end" && event.message?.role === "assistant") {
        for (const part of event.message.content ?? []) {
          if (part.type === "text") finalMessage = part.text;
        }
        output += finalMessage + "\n";
      }
    };

    proc.stdout.on("data", (d: Buffer) => {
      buffer += d.toString();
      const lines = buffer.split("\n");
      buffer = lines.pop() ?? "";
      for (const line of lines) processLine(line);
    });

    proc.stderr.on("data", (d: Buffer) => { output += d.toString(); });

    proc.on("close", (code: number | null) => {
      if (buffer.trim()) processLine(buffer);
      const success = code === 0 && finalMessage.trimEnd().endsWith("DONE");
      resolve({ success, finalMessage, output });
    });
  });
}

async function spawnDebugAgent(
  worktreePath: string,
  failureOutput: string,
): Promise<boolean> {
  return new Promise((resolve) => {
    const prompt = [
      `Tests failed in worktree: ${worktreePath}`,
      ``,
      `Failure output:`,
      failureOutput.slice(0, 4000),
      ``,
      `Diagnose and fix the failures. Run go test ./... after each fix attempt.`,
      `Do not create a PR.`,
      `When all tests pass, end your final message with exactly: DONE`,
    ].join("\n");

    const args = ["--mode", "json", "-p", "--no-session", prompt];
    const invocation = getPiInvocation(args);

    const proc = spawn(invocation.command, invocation.args, {
      cwd: worktreePath,
      stdio: ["ignore", "pipe", "pipe"],
      shell: false,
    });

    let finalMessage = "";
    let buffer = "";

    const processLine = (line: string) => {
      if (!line.trim()) return;
      let event: any;
      try { event = JSON.parse(line); } catch { return; }
      if (event.type === "message_end" && event.message?.role === "assistant") {
        for (const part of event.message.content ?? []) {
          if (part.type === "text") finalMessage = part.text;
        }
      }
    };

    proc.stdout.on("data", (d: Buffer) => {
      buffer += d.toString();
      const lines = buffer.split("\n");
      buffer = lines.pop() ?? "";
      for (const line of lines) processLine(line);
    });

    proc.on("close", (code: number | null) => {
      if (buffer.trim()) processLine(buffer);
      resolve(code === 0 && finalMessage.trimEnd().endsWith("DONE"));
    });
  });
}

export function registerWorkflow(pi: ExtensionAPI): void {
  pi.registerCommand("implement", {
    description: "Orchestrate headless sub-agents to implement the latest spec",
    handler: async (_args, ctx) => {
      await ctx.waitForIdle();

      const specPath = findLatestSpec(path.join(ctx.cwd, "docs"));
      if (!specPath) {
        ctx.ui.notify("No spec found in docs/superpowers/specs/", "error");
        return;
      }

      const specContent = fs.readFileSync(specPath, "utf-8");
      const phases = parsePhasesFromSpec(specContent);

      if (phases.length === 0) {
        ctx.ui.notify(
          "No phases found in spec. Add a ```phases JSON block to the spec.",
          "error",
        );
        return;
      }

      const specBase = path.basename(specPath, "-design.md").replace(/^\d{4}-\d{2}-\d{2}-/, "");
      const branch = `feat/${specBase}`;
      const worktreePath = path.join(ctx.cwd, ".worktrees", specBase);

      // Create worktree
      if (!fs.existsSync(worktreePath)) {
        try {
          fs.mkdirSync(path.dirname(worktreePath), { recursive: true });
          const { execSync } = await import("node:child_process");
          execSync(`git worktree add "${worktreePath}" -b "${branch}"`, {
            cwd: ctx.cwd,
            stdio: "pipe",
          });
        } catch (err: any) {
          ctx.ui.notify(`Failed to create worktree: ${err.message}`, "error");
          return;
        }
      }

      // Initialise TUI state
      const phaseStatuses: PhaseStatus[] = phases.map((p) => ({
        name: p.name,
        description: p.description,
        state: "pending" as const,
        progress: 0,
      }));

      setWorkflowState(
        { branch, phases: phaseStatuses, headless: "pending", e2e: "pending" },
        ctx,
      );

      ctx.ui.notify(`Starting ${phases.length} phases in ${branch}`, "info");

      // File lock map: glob pattern → phase name currently holding the lock
      const fileLocks = new Map<string, string>();
      const completed = new Set<string>();
      const failed = new Set<string>();

      async function runPhase(phase: PhaseDefinition): Promise<void> {
        // Acquire file locks
        for (const f of phase.files) fileLocks.set(f, phase.name);
        updatePhase(phase.name, { state: "running", progress: 0 }, ctx);

        const excerpt = specContent.slice(0, 3000);
        const result = await spawnPhaseAgent(phase, worktreePath, excerpt);

        // Release locks
        for (const f of phase.files) fileLocks.delete(f);

        if (result.success) {
          completed.add(phase.name);
          updatePhase(phase.name, { state: "done", progress: 1 }, ctx);
        } else {
          failed.add(phase.name);
          updatePhase(phase.name, { state: "failed" }, ctx);
          ctx.ui.notify(`Phase "${phase.name}" failed`, "error");
        }
      }

      // Scheduler: dispatch ready phases concurrently, repeat until all processed
      const pending = new Set(phases.map((p) => p.name));

      while (pending.size > 0) {
        // Find phases whose dependencies are all complete and whose files aren't locked
        const ready = phases.filter((p) => {
          if (!pending.has(p.name)) return false;
          if (!p.dependsOn.every((d) => completed.has(d))) return false;
          // Check file lock conflicts with currently running phases
          const currentlyLocked = Array.from(fileLocks.entries())
            .filter(([, owner]) => owner !== p.name)
            .map(([pattern]) => [pattern]);
          return !globOverlaps(p.files, currentlyLocked.flat());
        });

        if (ready.length === 0) {
          // All remaining phases are either waiting on deps or blocked by locks
          // Wait a bit and re-check (locks will be released as running phases finish)
          await new Promise((r) => setTimeout(r, 500));
          continue;
        }

        // Remove ready phases from pending and dispatch concurrently
        for (const p of ready) pending.delete(p.name);
        await Promise.all(ready.map(runPhase));
      }

      if (failed.size > 0) {
        ctx.ui.notify(`${failed.size} phase(s) failed. Manual intervention required.`, "error");
        return;
      }

      // All phases done — run two-layer tests
      let testResults = await runAllTests(worktreePath, ctx);
      let debugIterations = 0;
      const MAX_DEBUG = 3;

      while (!testResults.every((r) => r.passed) && debugIterations < MAX_DEBUG) {
        debugIterations++;
        updateTestState({ debugIteration: debugIterations }, ctx);
        ctx.ui.notify(`Tests failed — debug attempt ${debugIterations}/${MAX_DEBUG}`, "warning");

        const failureOutput = testResults.map(captureTestOutput).join("\n");
        await spawnDebugAgent(worktreePath, failureOutput);

        testResults = await runAllTests(worktreePath, ctx);
      }

      if (!testResults.every((r) => r.passed)) {
        ctx.ui.notify("Tests still failing after 3 debug iterations. Manual intervention required.", "error");
        return;
      }

      // Create PR
      updateTestState({ debugIteration: undefined }, ctx);
      const specTitle = specBase.replace(/-/g, " ");

      try {
        const { execSync } = await import("node:child_process");

        execSync(`git push -u origin "${branch}"`, {
          cwd: worktreePath,
          stdio: "pipe",
        });

        const prOutput = execSync(
          `gh pr create --title "${specTitle}" --body "Implements: ${specBase}\n\nSee docs/superpowers/specs/${path.basename(specPath)}" --head "${branch}"`,
          { cwd: worktreePath, encoding: "utf-8", stdio: "pipe" },
        );

        const prMatch = prOutput.match(/#(\d+)/);
        const prNumber = prMatch ? parseInt(prMatch[1], 10) : undefined;

        updateTestState({ prNumber }, ctx);
        ctx.ui.notify(`PR created${prNumber ? ` #${prNumber}` : ""}`, "success");

        // Clear widget after 5 seconds
        setTimeout(() => setWorkflowState(null, ctx), 5000);
      } catch (err: any) {
        ctx.ui.notify(`PR creation failed: ${err.message}`, "error");
      }
    },
  });
}
