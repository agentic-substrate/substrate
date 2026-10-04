import { defineConfig } from "vitepress";

export default defineConfig({
  title: "Substrate",
  description:
    "Durable context across coding harnesses, sessions, and machines.",
  cleanUrls: true,
  themeConfig: {
    nav: [
      { text: "Vision", link: "/product/vision" },
      { text: "Start", link: "/getting-started" },
      { text: "Development", link: "/development" },
      { text: "Architecture", link: "/architecture/" },
      {
        text: "Source",
        link: "https://github.com/agentic-substrate/substrate",
      },
    ],
    sidebar: [
      { text: "Overview", link: "/" },
      { text: "Getting started", link: "/getting-started" },
      { text: "Development", link: "/development" },
      {
        text: "Product direction",
        items: [
          { text: "Vision", link: "/product/vision" },
          { text: "Problem", link: "/product/problem" },
          { text: "Positioning", link: "/product/positioning" },
          { text: "Non-goals", link: "/product/non-goals" },
          { text: "First release", link: "/product/first-release" },
        ],
      },
      { text: "Architecture decisions", link: "/architecture/" },
    ],
    outline: [2, 3],
  },
  vite: { server: { host: "127.0.0.1" } },
});
