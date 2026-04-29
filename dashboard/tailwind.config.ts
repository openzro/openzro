import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./node_modules/flowbite-react/**/*.js",
    "./src/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        // Dark scale shifted from upstream NetBird's neutral grays to a
        // violet-undertone palette — same lightness curve, but every
        // shade leans toward the brand violet (--oz-ink at the bottom,
        // gradually less violet as the scale lightens). Keeps the
        // sidebar / cards visibly "openZro" without going saturated
        // purple. CLAUDE.md tokens `--oz-ink` (#0f0a1f) and
        // `--oz-ink-2` (#1a1330) anchor the deep end. Light end stays
        // close to neutral so light-mode (still WIP) reads correctly.
        "nb-gray": {
          DEFAULT: "#0f0a1f",
          "50":  "#f1f1f4",
          "100": "#e3e3eb",
          "200": "#d0d0db",
          "250": "#c1c1d0",
          "300": "#b4b3c6",
          "350": "#a7a6b8",
          "400": "#9594a8",
          "500": "#75748b",
          "600": "#605f78",
          "700": "#535168",
          "800": "#403e60",
          "850": "#3a3658",
          "900": "#332f50",
          "910": "#2c2848",
          "920": "#252040",
          "925": "#1f1838",
          "930": "#1a1330",
          "940": "#13102a",
          "950": "#0f0a1f",
        },
        // The `openzro` palette name is preserved (every existing
        // component uses bg-openzro-500, text-openzro-400, etc.) but
        // the values are now the violet scale from CLAUDE.md /
        // design-tokens.md. Only the hex values changed; class names
        // stay identical so we don't have to touch every component.
        // The 150 and 950 stops are interpolated; everything else is
        // the canonical violet scale verbatim.
        openzro: {
          DEFAULT: "#7c3aed", // violet-600 — brand primary
          "50": "#f5f3ff",
          "100": "#ede9fe",
          "150": "#e7e1fc",
          "200": "#ddd6fe",
          "300": "#c4b5fd",
          "400": "#a78bfa",
          "500": "#8b5cf6",
          "600": "#7c3aed",
          "700": "#6d28d9",
          "800": "#5b21b6",
          "900": "#4c1d95",
          "950": "#2e1065",
        },
        // Ink (neutrals, dark surfaces) and paper (off-white background)
        // straight from the brand spec. Available as bg-oz-ink etc.
        "oz-ink": "#0f0a1f",
        "oz-ink-2": "#1a1330",
        "oz-paper": "#faf9fc",
        "nb-blue": {
          DEFAULT: "#31e4f5",
          "50": "#ebffff",
          "100": "#cefdff",
          "200": "#a2f9ff",
          "300": "#63f2fd",
          "400": "#31e4f5",
          "500": "#00c4da",
          "600": "#039cb7",
          "700": "#0a7c94",
          "800": "#126478",
          "900": "#145365",
          "950": "#063746",
        },
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
      },
      animation: {
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up": "accordion-up 0.2s ease-out",
      },
      transitionDuration: {
        "3000": "3000ms",
      },
      fontFamily: {
        // CSS variables wired up in src/app/layout.tsx via next/font.
        sans: ["var(--font-geist-sans)", "ui-sans-serif", "system-ui", "sans-serif"],
        mono: ["var(--font-jetbrains-mono)", "ui-monospace", "SFMono-Regular", "monospace"],
      },
    },
  },
  plugins: [require("flowbite/plugin"), require("tailwindcss-animate")],
};
export default config;
