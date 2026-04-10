# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Universal Search — a local-first, cross-platform desktop app that indexes files on disk using Gemini Embedding 2 and provides instant semantic search via a global keyboard shortcut. Built with Wails v2 (Go backend + React frontend).

Design spec: `docs/superpowers/specs/2026-03-27-universal-search-design.md`

## Tech Stack

- **Desktop framework:** Wails v2
- **Frontend:** React + TypeScript
- **Backend:** Go
- **Vector DB:** TFMV/hnsw (pure Go, HNSW algorithm)
- **Metadata DB:** SQLite via ncruces/go-sqlite3 (pure Go, no CGO)
- **Embeddings:** Gemini Embedding 2 Preview (`gemini-embedding-2-preview`, 768-dim via MRL)
- **Video processing:** ffmpeg (subprocess)
- **File watching:** fsnotify

## Architecture

The app has three main layers:

1. **React frontend** — Raycast-style floating search window with thumbnail-based results list (left) and smart context-aware preview panel (right). Collapsible indexing progress bar at the bottom.
2. **Go backend** — File watcher, indexing pipeline (classify → extract/chunk → embed via Gemini API → store), search engine (embed query → HNSW cosine search → join metadata), system tray management. Key App methods: `GetIgnoredFolders`, `AddIgnoredFolder`, `RemoveIgnoredFolder` (manage excluded folder name patterns); `GetFolders`, `AddFolder`, `RemoveFolder` (manage indexed folders); `GetSetting`/`SetSetting` (key-value settings).
3. **Storage** — SQLite for file metadata (paths, types, hashes, chunk timestamps, thumbnails), indexed folders, excluded patterns, and key-value settings. TFMV/hnsw for 768-dim vector embeddings. Separate concerns: SQLite is the source of truth for "what files exist", HNSW is the search index. On first launch, 20 default ignore patterns are seeded into the `excluded_patterns` table (e.g. `node_modules`, `.git`, `venv`).

Video pipeline follows sentrysearch patterns: 30s chunks with 5s overlap, 480p/5fps preprocessing, still-frame detection, direct video-to-embedding (no captioning).

## Rules

- **Before writing any code, verify the correct syntax and API usage.** Use Context7 MCP (`resolve-library-id` then `query-docs`) or fetch official documentation to confirm function signatures, package APIs, and framework patterns. Do not rely on memory alone — always reference current docs.
- All Go dependencies must be **pure Go (no CGO)** unless absolutely unavoidable. This is critical for cross-platform builds.
- Embedding spaces are **incompatible across models**. Never mix embeddings from different Gemini model versions.
- Use `RETRIEVAL_DOCUMENT` task type for indexing, `RETRIEVAL_QUERY` for search queries.
- FFmpeg is called as a subprocess, not via Go bindings.
- **Do not add Claude as a co-author in commit messages.** No `Co-Authored-By` lines.
- **Never commit docs, specs, or plans.** Files in `docs/` (design specs, plans, research notes) are local-only and must not be committed to git.

## Build & Run

```bash
# Install Wails CLI (one-time)
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Development mode (hot reload)
# -tags webkit2_41 required on Linux with webkit2gtk-4.1 (Ubuntu 22.04+)
wails dev -tags webkit2_41

# Build production binary
wails build -tags webkit2_41

# Run Go tests
go test ./...

# Run a single test
go test ./internal/indexer -run TestChunkVideo

# Frontend dev (standalone, if needed)
cd frontend && npm install && npm run dev

# Lint
cd frontend && npm run lint
```

## Git rules

- Never commit docs/ — these are local working files, not part of the deliverable.
- Never add Co-Authored-By or any AI attribution to commits.
- No emojis in commit messages.
- Write commit messages in a natural, human tone — as if the developer wrote them. Keep them concise and descriptive.