import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import type { Connect } from 'vite'
import type { ServerResponse, IncomingMessage } from 'node:http'

// In `wails dev`, the frontend is served by this Vite dev server and Wails's
// AssetServer proxies to it. Vite's SPA fallback otherwise returns index.html
// for unknown paths, which prevents Wails's custom Handler (our
// localFileHandler in main.go) from ever running — video/image previews that
// rely on `/localfile/*` then receive HTML and render as blank.
//
// Rejecting `/localfile/*` here with a 404 makes Wails's AssetServer fall
// through to the registered Handler, which serves the actual file from disk.
function localFileFallthrough(): Connect.NextHandleFunction {
  return (req: IncomingMessage, res: ServerResponse, next: (err?: unknown) => void) => {
    if (req.url && req.url.startsWith('/localfile/')) {
      res.statusCode = 404
      res.end()
      return
    }
    next()
  }
}

export default defineConfig({
  plugins: [
    react(),
    {
      name: 'localfile-fallthrough',
      configureServer(server) {
        server.middlewares.use(localFileFallthrough())
      },
    },
  ],
})
