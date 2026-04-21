# Universal Search

A local-first, cross-platform desktop app that indexes files on disk using Gemini Embedding 2 and provides instant semantic search via a global keyboard shortcut. Built with Wails v2 (Go backend + React frontend).

Search by meaning, not filenames. "people on stage" finds your conference videos. "revenue chart" finds that buried PowerPoint slide.

## Features

- Semantic search across all your files — text, images, videos, audio, documents
- Raycast-style floating search window with thumbnail previews
- Local-first: files never leave your machine (only embedding API calls)
- Real-time file watching with automatic re-indexing; crash-safe indexing with atomic HNSW saves and startup reconciliation
- Video search with timestamp-level precision (30s chunked embedding)
- Cross-format search — one query searches everything
- Configurable ignored folders: add/remove folder name patterns to skip during indexing (20 defaults seeded on first launch, e.g. `node_modules`, `.git`, `venv`)

## Supported File Formats

### No external tools needed

**Images**
| Extension | Format |
|---|---|
| `.jpg` `.jpeg` | JPEG |
| `.png` | PNG |
| `.webp` | WebP |
| `.heic` | HEIC (iPhone photos) |
| `.heif` | HEIF |

**Documents**
| Extension | Format | Method |
|---|---|---|
| `.pdf` | PDF | Gemini visual embedding + text extraction |
| `.docx` | Word | XML text extraction |
| `.pptx` | PowerPoint | XML text extraction |
| `.xlsx` | Excel | Full cell/sheet extraction via excelize |

**Text / Code** — auto-detected via content sniffing (no hardcoded extension list)
| Examples | |
|---|---|
| `.go` `.py` `.js` `.ts` `.tsx` `.rs` `.c` `.cpp` `.java` `.rb` `.php` `.swift` `.kt` `.scala` `.lua` `.zig` `.sql` `.r` `.m` `.pl` | Code |
| `.md` `.txt` `.html` `.css` `.xml` `.json` `.yaml` `.yml` `.toml` `.csv` `.ini` `.env` `.cfg` `.log` | Config / Markup |
| `.tex` `.rst` `.proto` `.sh` `.bash` `.vue` `.jsx` `.svelte` | Other text |
| Any file with valid UTF-8 and no null bytes | Auto-detected |

### Requires ffmpeg

**Video** — 30-second chunks with 5s overlap, preprocessed to 480p/5fps
| Extension | Format |
|---|---|
| `.mp4` | MP4 |
| `.mov` | QuickTime |
| `.avi` | AVI |
| `.webm` | WebM |
| `.mpeg` `.mpg` | MPEG |
| `.flv` | Flash Video |
| `.wmv` | Windows Media |
| `.3gp` | 3GPP |

**Audio**
| Extension | Format |
|---|---|
| `.mp3` | MP3 |
| `.wav` | WAV |
| `.flac` | FLAC |
| `.aac` | AAC |
| `.ogg` | Ogg Vorbis |
| `.aiff` | AIFF |

### Requires LibreOffice (optional)

These legacy formats need LibreOffice installed for conversion to PDF before embedding. Modern Office formats (`.docx`, `.pptx`, `.xlsx`) work without it.

| Extension | Format |
|---|---|
| `.doc` | Word 97-2003 |
| `.ppt` | PowerPoint 97-2003 |
| `.xls` | Excel 97-2003 |
| `.odt` | OpenDocument Text |
| `.odp` | OpenDocument Presentation |
| `.ods` | OpenDocument Spreadsheet |
| `.rtf` | Rich Text Format |

## Prerequisites

- [Go](https://go.dev/) 1.26+
- [Wails CLI](https://wails.io/) v2
- [Node.js](https://nodejs.org/) 18+
- A Gemini API key (`GEMINI_API_KEY` environment variable)
- [ffmpeg](https://ffmpeg.org/) — required for video/audio indexing
- [LibreOffice](https://www.libreoffice.org/) — optional, for legacy Office format support

## Build & Run

```bash
# Install Wails CLI (one-time)
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Development mode (hot reload)
# -tags webkit2_41 required on Linux with webkit2gtk-4.1 (Ubuntu 22.04+)
wails dev -tags webkit2_41

# Build production binary
wails build -tags webkit2_41

# Run tests (three tiers)
make test-unit          # default build, under 30s
make test-integration   # -tags integration
make test-e2e           # -tags e2e
make test-all           # all three, sequentially
```

## Configuration

Runtime tunables (indexing concurrency, embedder batch size, rate limits, HNSW parameters, search thresholds, NL-query timeouts) live in `config.toml`:

- Linux/macOS: `~/.config/universal-search/config.toml`
- Windows: `%APPDATA%\universal-search\config.toml`

Missing file or missing keys fall back to the defaults in `internal/config/defaults.toml`.

## Tech Stack

- **Desktop framework:** Wails v2
- **Frontend:** React + TypeScript
- **Backend:** Go
- **Vector DB:** TFMV/hnsw (pure Go, HNSW algorithm)
- **Metadata DB:** SQLite via ncruces/go-sqlite3 (pure Go, no CGO)
- **Embeddings:** Gemini Embedding 2 Preview (768-dim)
- **Video processing:** ffmpeg (subprocess)
- **File watching:** fsnotify
- **Excel parsing:** excelize

## Rules
- Do NOT write comments unless required. 

## Git rules

- Never commit docs/ — these are local working files, not part of the deliverable.
- Never add Co-Authored-By or any AI attribution to commits.
- No emojis in commit messages.
- Write commit messages in a natural, human tone — as if the developer wrote them. Keep them concise and descriptive.