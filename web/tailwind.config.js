import { nextui } from "@nextui-org/react";

/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
    "./node_modules/@nextui-org/theme/dist/**/*.{js,ts,jsx,tsx}",
  ],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        e2b: {
          dark: "#070709",
          surface: "#0d0e12",
          "surface-elevated": "#14161d",
          "surface-hover": "#1a1d27",
          line: "rgba(255, 255, 255, 0.08)",
          "line-subtle": "rgba(255, 255, 255, 0.04)",
          orange: "#ff5722",
          "orange-hover": "#ff6e40",
          "orange-glow": "rgba(255, 87, 34, 0.25)",
        },
      },
      fontFamily: {
        mono: [
          "CommitMono",
          "JetBrains Mono",
          "ui-monospace",
          "SFMono-Regular",
          "Menlo",
          "Monaco",
          "Consolas",
          "monospace",
        ],
        sans: [
          "Inter",
          "-apple-system",
          "BlinkMacSystemFont",
          "Segoe UI",
          "Roboto",
          "Helvetica Neue",
          "sans-serif",
        ],
      },
      boxShadow: {
        "geek-glow": "0 0 20px -3px rgba(255, 87, 34, 0.25)",
        "geek-green": "0 0 15px -3px rgba(34, 197, 94, 0.3)",
        "geek-card": "0 0 0 1px rgba(255, 255, 255, 0.07), 0 4px 20px -2px rgba(0, 0, 0, 0.6)",
      },
    },
  },
  plugins: [
    nextui({
      prefix: "nextui",
      defaultTheme: "light",
      defaultExtendTheme: "light",
      themes: {
        dark: {
          colors: {
            background: "#070709",
            foreground: "#ededed",
            focus: "#ff5722",
            primary: {
              50: "#fff7ed",
              100: "#ffedd5",
              200: "#fed7aa",
              300: "#fdba74",
              400: "#fb923c",
              500: "#ff5722",
              600: "#ea580c",
              700: "#c2410c",
              800: "#9a3412",
              900: "#7c2d12",
              DEFAULT: "#ff5722",
              foreground: "#ffffff",
            },
            content1: "#0d0e12",
            content2: "#14161d",
            content3: "#1a1d27",
            content4: "#242836",
            default: {
              100: "#181a22",
              200: "#222530",
              300: "#2e3240",
              400: "#3d4255",
              500: "#71717a",
              DEFAULT: "#222530",
              foreground: "#ededed",
            },
            success: {
              DEFAULT: "#22c55e",
              foreground: "#000000",
            },
            danger: {
              DEFAULT: "#ef4444",
              foreground: "#ffffff",
            },
            warning: {
              DEFAULT: "#f59e0b",
              foreground: "#000000",
            },
          },
          layout: {
            radius: {
              small: "4px",
              medium: "6px",
              large: "8px",
            },
            borderWidth: {
              small: "1px",
              medium: "1px",
              large: "1.5px",
            },
          },
        },
        light: {
          colors: {
            background: "#f4f5f8",
            foreground: "#0f172a",
            focus: "#ff5722",
            primary: {
              50: "#fff7ed",
              100: "#ffedd5",
              200: "#fed7aa",
              300: "#fdba74",
              400: "#fb923c",
              500: "#ff5722",
              600: "#ea580c",
              700: "#c2410c",
              800: "#9a3412",
              900: "#7c2d12",
              DEFAULT: "#ff5722",
              foreground: "#ffffff",
            },
            content1: "#ffffff",
            content2: "#f8f9fa",
            content3: "#f1f3f5",
            content4: "#e9ecef",
            default: {
              100: "#f1f3f5",
              200: "#e9ecef",
              300: "#dee2e6",
              400: "#ced4da",
              500: "#64748b",
              DEFAULT: "#e2e8f0",
              foreground: "#0f172a",
            },
            success: {
              DEFAULT: "#16a34a",
              foreground: "#ffffff",
            },
            danger: {
              DEFAULT: "#dc2626",
              foreground: "#ffffff",
            },
            warning: {
              DEFAULT: "#d97706",
              foreground: "#ffffff",
            },
          },
          layout: {
            radius: {
              small: "4px",
              medium: "6px",
              large: "8px",
            },
            borderWidth: {
              small: "1px",
              medium: "1px",
              large: "1.5px",
            },
          },
        },
      },
    }),
  ],
};
