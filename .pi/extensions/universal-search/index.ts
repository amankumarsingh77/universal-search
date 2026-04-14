import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { registerContextInjection } from "./context.js";
import { registerGuards } from "./guards.js";
import { registerTUI } from "./tui.js";
import { registerTestRunner } from "./test-runner.js";
import { registerWorkflow } from "./workflow.js";

export default function (pi: ExtensionAPI) {
  registerContextInjection(pi);
  registerGuards(pi);
  registerTUI(pi);
  registerTestRunner(pi);
  registerWorkflow(pi);
}
