import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue2";
import { viteSingleFile } from "vite-plugin-singlefile";

// The UI is bundled into a single self-contained index.html so the standalone
// bundle works when opened directly over file:// (no module-CORS issues), and
// so the Go binary embeds one asset. Build it with bun (no Node needed):
//   bun install && bun --bun vite build
export default defineConfig({
  base: "./",
  plugins: [vue(), viteSingleFile()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  build: { outDir: "dist", emptyOutDir: true },
  // dev-server proxy for `bun run dev` against a locally running ponto server
  server: { proxy: { "/api": "http://localhost:7668" } },
});
