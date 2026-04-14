import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

export interface PhaseStatus {
  name: string;
  description: string;
  state: "pending" | "running" | "done" | "blocked" | "failed";
  progress: number; // 0.0 – 1.0
}

export interface WorkflowState {
  branch: string;
  phases: PhaseStatus[];
  headless: "pending" | "running" | "passed" | "failed";
  headlessCount?: { pass: number; fail: number };
  e2e: "pending" | "running" | "passed" | "failed";
  prNumber?: number;
  debugIteration?: number;
}

let currentState: WorkflowState | null = null;

export function registerTUI(pi: ExtensionAPI): void {
  pi.on("session_start", async (_event, ctx) => {
    renderFooter(ctx);
  });
}

function renderFooter(ctx: { ui: any }): void {
  if (!currentState) {
    ctx.ui.setStatus("us", dimText("[us] ") + mutedText("no active task"));
    return;
  }

  const s = currentState;
  const branch = accentText(truncate(s.branch, 30));

  const phaseSummary = s.phases
    .map((p, i) => {
      const label = `p${i + 1}`;
      if (p.state === "done") return successText(`${label}✓`);
      if (p.state === "running") return warningText(`${label}⏳`);
      if (p.state === "failed") return errorText(`${label}✗`);
      if (p.state === "blocked") return warningText(`${label}⏸`);
      return mutedText(`${label}–`);
    })
    .join(" ");

  let testSummary = "";
  if (s.headless === "pending") testSummary += mutedText("headless:–");
  else if (s.headless === "running") testSummary += warningText("headless running…");
  else if (s.headless === "passed") testSummary += successText(`✓ ${s.headlessCount?.pass ?? "?"} headless`);
  else if (s.headless === "failed") testSummary += errorText(`✗ headless ${s.headlessCount?.fail ?? "?"}`);

  testSummary += "  ";

  if (s.e2e === "pending") testSummary += mutedText("e2e:–");
  else if (s.e2e === "running") testSummary += warningText("e2e ⏳");
  else if (s.e2e === "passed") testSummary += successText("✓ e2e");
  else if (s.e2e === "failed") testSummary += errorText("✗ e2e");

  let extra = "";
  if (s.prNumber) extra = "  " + accentText(`PR #${s.prNumber}`);
  if (s.debugIteration) extra = "  " + warningText(`debugging… (${s.debugIteration}/3)`);

  ctx.ui.setStatus("us", `${dimText("[us]")} ${branch}  ${phaseSummary}  ${testSummary}${extra}`);
}

function renderWidget(ctx: { ui: any }): void {
  if (!currentState) {
    ctx.ui.setWidget("us", undefined);
    return;
  }

  const s = currentState;

  ctx.ui.setWidget("us", (_tui: any, theme: any) => {
    const lines: string[] = [];
    const BAR_WIDTH = 8;

    const borderColor = (t: string) => theme.fg("borderAccent", t);
    const branchLabel = ` ${s.branch} `;
    const totalWidth = 48;
    const dashCount = Math.max(0, totalWidth - branchLabel.length - 2);
    lines.push(borderColor(`┌${branchLabel}`) + borderColor("─".repeat(dashCount)) + borderColor("┐"));

    for (const phase of s.phases) {
      const label = theme.fg("text", phase.name.padEnd(20).slice(0, 20));

      let statusStr = "";
      if (phase.state === "done") {
        statusStr = theme.fg("success", "[done        ✓]");
      } else if (phase.state === "running") {
        const filled = Math.round(phase.progress * BAR_WIDTH);
        const empty = BAR_WIDTH - filled;
        const bar = theme.fg("accent", "█".repeat(filled)) + theme.fg("dim", " ".repeat(empty));
        statusStr = theme.fg("muted", "[") + theme.fg("warning", "running ") + bar + theme.fg("muted", "]");
      } else if (phase.state === "blocked") {
        statusStr = theme.fg("warning", "[blocked     ⏸]");
      } else if (phase.state === "failed") {
        statusStr = theme.fg("error", "[failed      ✗]");
      } else {
        statusStr = theme.fg("muted", "[pending      ]");
      }

      lines.push(theme.fg("border", "│") + " " + label + " " + statusStr + " " + theme.fg("border", "│"));
    }

    const sep = theme.fg("border", "│");
    let hl = "  headless  ";
    if (s.headless === "pending") hl += theme.fg("muted", "–");
    else if (s.headless === "running") hl += theme.fg("warning", "running…");
    else if (s.headless === "passed") hl += theme.fg("success", `✓ ${s.headlessCount?.pass ?? "?"}`);
    else hl += theme.fg("error", `✗ ${s.headlessCount?.fail ?? "?"}`);

    hl += "    e2e  ";
    if (s.e2e === "pending") hl += theme.fg("muted", "–");
    else if (s.e2e === "running") hl += theme.fg("warning", "running…");
    else if (s.e2e === "passed") hl += theme.fg("success", "✓ passed");
    else hl += theme.fg("error", "✗ failed");

    lines.push(`${sep}${hl.padEnd(46)}${sep}`);
    lines.push(borderColor("└" + "─".repeat(totalWidth - 2) + "┘"));

    return {
      render: () => lines,
      invalidate: () => {},
    };
  });
}

// ANSI helpers matching the theme vars
function dimText(s: string): string { return `\x1b[38;5;238m${s}\x1b[0m`; }
function mutedText(s: string): string { return `\x1b[38;5;242m${s}\x1b[0m`; }
function accentText(s: string): string { return `\x1b[38;2;79;195;247m${s}\x1b[0m`; }
function successText(s: string): string { return `\x1b[38;2;129;201;149m${s}\x1b[0m`; }
function errorText(s: string): string { return `\x1b[38;2;242;139;130m${s}\x1b[0m`; }
function warningText(s: string): string { return `\x1b[38;2;253;214;99m${s}\x1b[0m`; }
function truncate(s: string, n: number): string { return s.length > n ? s.slice(0, n - 1) + "…" : s; }

// Public API — called by workflow.ts and test-runner.ts
export function setWorkflowState(state: WorkflowState | null, ctx: { ui: any }): void {
  currentState = state;
  renderFooter(ctx);
  if (state) {
    renderWidget(ctx);
  } else {
    ctx.ui.setWidget("us", undefined);
  }
}

export function updatePhase(
  phaseName: string,
  update: Partial<PhaseStatus>,
  ctx: { ui: any },
): void {
  if (!currentState) return;
  const phase = currentState.phases.find((p) => p.name === phaseName);
  if (!phase) return;
  Object.assign(phase, update);
  renderFooter(ctx);
  renderWidget(ctx);
}

export function updateTestState(
  update: Partial<Pick<WorkflowState, "headless" | "headlessCount" | "e2e" | "prNumber" | "debugIteration">>,
  ctx: { ui: any },
): void {
  if (!currentState) return;
  Object.assign(currentState, update);
  renderFooter(ctx);
  renderWidget(ctx);
}
