import { isToolCallEventType } from "@mariozechner/pi-coding-agent";
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

const TEMP_DIRS = ["/tmp", "/var/folders", "test-", ".worktrees/"];

function isInTempDir(command: string): boolean {
  return TEMP_DIRS.some((dir) => command.includes(dir));
}

export function registerGuards(pi: ExtensionAPI): void {
  pi.on("tool_call", async (event, ctx) => {
    if (!isToolCallEventType("bash", event)) return;

    const cmd = event.input.command ?? "";

    // Hard block: force push
    if (/git\s+push\s+(--force|-f)\b/.test(cmd)) {
      return { block: true, reason: "Force push blocked by harness. Use a regular push." };
    }

    // Hard block: Co-Authored-By in commit message
    if (/git\s+commit/.test(cmd) && /Co-Authored-By/i.test(cmd)) {
      return { block: true, reason: "Co-Authored-By not allowed in commit messages." };
    }

    // Hard block: emoji in commit message
    if (/git\s+commit/.test(cmd) && /[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}]/u.test(cmd)) {
      return { block: true, reason: "Emojis not allowed in commit messages." };
    }

    // Hard block: direct write to go.sum
    if (/\bgo\.sum\b/.test(cmd) && /(echo|tee|cat\s*>|sed\s+-i)/.test(cmd)) {
      return {
        block: true,
        reason: "go.sum must only be modified via go mod commands (go get, go mod tidy).",
      };
    }

    // Soft gate: rm -rf outside temp dirs
    if (/rm\s+(-[rfRF]+\s+|--recursive\s+)/.test(cmd) && !isInTempDir(cmd)) {
      const ok = await ctx.ui.confirm(
        "Destructive command",
        `rm -rf outside a temp directory:\n\n${cmd}\n\nAllow?`,
      );
      if (!ok) return { block: true, reason: "Blocked by user." };
    }
  });
}
