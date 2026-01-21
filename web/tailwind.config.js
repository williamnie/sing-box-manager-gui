import { nextui } from "@nextui-org/react";

/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
    "./node_modules/@nextui-org/theme/dist/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      boxShadow: {
        'sm': '0 1px 2px 0 rgba(0, 0, 0, 0.02)',
        'DEFAULT': '0 1px 2px 0 rgba(0, 0, 0, 0.03)',
        'md': '0 2px 4px 0 rgba(0, 0, 0, 0.04)',
        'lg': '0 4px 6px -1px rgba(0, 0, 0, 0.05)',
        'xl': '0 8px 12px -2px rgba(0, 0, 0, 0.06)',
        'none': 'none',
      },
    },
  },
  darkMode: "class",
  plugins: [
    nextui({
      themes: {
        light: {
          colors: {},
        },
        dark: {
          colors: {},
        },
      },
      layout: {
        radius: {
          small: "8px",
          medium: "12px",
          large: "14px",
        },
        boxShadow: {
          small: "none",
          medium: "0 1px 2px 0 rgba(0, 0, 0, 0.02)",
          large: "0 2px 4px 0 rgba(0, 0, 0, 0.03)",
        },
        borderWidth: {
          small: "1px",
          medium: "1px",
          large: "1px",
        },
      },
    }),
  ],
};
