import { defineConfig } from "vitepress";

export default defineConfig({
  title: "Substrate",
  description: "Permitted artifacts across coding harnesses.",
  cleanUrls: true,
  themeConfig: {
    nav: [
      { text: "Start", link: "/getting-started" },
      { text: "Development", link: "/development" },
      { text: "Architecture", link: "/architecture/" },
      { text: "Source", link: "https://github.com/agentic-substrate/substrate" },
    ],
    sidebar: [
      { text: "Overview", link: "/" },
      { text: "Getting started", link: "/getting-started" },
      { text: "Development", link: "/development" },
      { text: "First release", link: "/product/first-release" },
      { text: "Architecture decisions", link: "/architecture/" },
    ],
    outline: [2, 3],
  },
  vite: { server: { host: "127.0.0.1" } },
});
