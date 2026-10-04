import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  root: "web",
  plugins: [react()],
  build: { outDir: "../internal/webui/dist", emptyOutDir: true },
  server: { host: "127.0.0.1", proxy: { "/api": "http://127.0.0.1:9842" } },
});
