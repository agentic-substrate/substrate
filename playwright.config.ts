import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./web/tests",
  forbidOnly: true,
  retries: 0,
  reporter: [["list"], ["json", { outputFile: "test-results/browser.json" }]],
  use: { baseURL: "http://127.0.0.1:9842", browserName: "chromium" },
  webServer: {
    command: "./bin/substrate serve -listen 127.0.0.1:9842",
    url: "http://127.0.0.1:9842/api/status",
    reuseExistingServer: false,
    timeout: 10_000,
  },
});
