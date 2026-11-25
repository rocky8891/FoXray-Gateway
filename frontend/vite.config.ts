import { gzipSync } from "node:zlib";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig({
  plugins: [react(), {
    name: "foxray-compressed-assets",
    apply: "build",
    generateBundle(_options, bundle) {
      for (const [name, asset] of Object.entries(bundle)) {
        if (!name.startsWith("assets/") || !/\.(?:js|css)$/.test(name)) continue;
        const source = asset.type === "chunk" ? asset.code : asset.source;
        if (source.length >= 1024) this.emitFile({ type: "asset", fileName: name + ".gz", source: gzipSync(source, { level: 9 }) });
      }
    },
  }],
  worker: { format: "es" },
  server: { proxy: { "/api": "http://192.168.1.8:18880" } },
  build: { outDir: "dist", emptyOutDir: true },
});
