import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { execSync } from "node:child_process";

export function registerContextInjection(pi: ExtensionAPI): void {
  pi.on("before_agent_start", async (event, ctx) => {
    let branch = "unknown";

    try {
      branch = execSync("git rev-parse --abbrev-ref HEAD", {
        cwd: ctx.cwd,
        encoding: "utf-8",
        stdio: ["pipe", "pipe", "pipe"],
      }).trim();
    } catch {
      // not a git repo or git not available
    }

    const rules = [
      `Current branch: ${branch}`,
      `Working directory: ${ctx.cwd}`,
      "",
      "Universal Search project rules (enforced by harness):",
      "- All Go dependencies must be pure Go. No CGO. Verify with: grep -r 'import \"C\"' --include='*.go' .",
      "- Never add Co-Authored-By or AI attribution to commit messages.",
      "- Never commit files under docs/ — specs and plans are local-only.",
      "- Commit messages: natural human tone, concise, no emojis.",
      "- Use RETRIEVAL_DOCUMENT task type for indexing, RETRIEVAL_QUERY for search queries.",
      "- Never mix embeddings from different Gemini model versions.",
      "- FFmpeg must be called as a subprocess only, never via Go bindings.",
      "- Before writing any Go code, verify API usage via context7 MCP or official docs.",
    ].join("\n");

    return {
      systemPrompt: event.systemPrompt + "\n\n" + rules,
    };
  });
}
