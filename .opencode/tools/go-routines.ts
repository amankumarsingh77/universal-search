import { tool } from "@opencode-ai/plugin"
import path from "path"

export default tool({
  description: "Audit Go files for goroutine-safety and concurrency issues",
  args: {
    directory: tool.schema.string().optional().describe("Directory to audit (default: internal/)"),
  },
  async execute(args, context) {
    const dir = args.directory || "internal/"
    const target = path.join(context.worktree, dir)
    const goType = "--type go"

    const goStarts = await Bun.$`rg -n "go\\s+(func|\\w+\\()" ${goType} ${target}`.quiet().text().catch(() => "")
    const channels = await Bun.$`rg -n "make\\(chan" ${goType} ${target}`.quiet().text().catch(() => "")
    const mutexes = await Bun.$`rg -n "sync\\.(RWMutex|Mutex)\\b" ${goType} ${target}`.quiet().text().catch(() => "")
    const ctxInStructs = await Bun.$`rg -n "ctx\\b.*context\\.Context" --type go ${target}`.quiet().text().catch(() => "")
    const largeChans = await Bun.$`rg -n "make\\(chan.*,\\s*[2-9]|[1-9][0-9]+" ${goType} ${target}`.quiet().text().catch(() => "")

    return `## Goroutine & Concurrency Audit (${dir})

### Goroutines
Check: every goroutine must have a stop mechanism.
${goStarts.trim() || "None found"}

### Channel Creations
Check: buffer size must be 0 or 1. Anything larger needs a comment.
${channels.trim() || "None found"}

### Large Channel Buffers (>1) — LIKELY ISSUES
${largeChans.trim() || "None found — clean"}

### Mutex Declarations
Check: named fields only (mu sync.Mutex), never embedded directly.
${mutexes.trim() || "None found"}

### Context in Struct Fields
Check: context must be first parameter, never stored in structs.
${ctxInStructs.trim() || "None found — clean"}

---
Review each finding. For every goroutine, verify it has a predictable stop mechanism (context cancellation, done channel, or errgroup).`
  },
})
