import { defineConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import tailwindcss from "@tailwindcss/vite";
import wails from "@wailsio/runtime/plugins/vite";
import path from "path";

export default defineConfig({
  plugins: [react(), tailwindcss(), wails("./bindings")],
  server: {
    host: "127.0.0.1",
    strictPort: true,
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
});
