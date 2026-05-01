import type { Config } from "tailwindcss";
// Tailwind plugins ship as CommonJS. Default-imports + esModuleInterop
// (set in tsconfig.json) lets us pull them in without a `require()`,
// which broke at runtime once Next.js started loading this file as
// ESM.
import flowbitePlugin from "flowbite/plugin";
import tailwindcssAnimate from "tailwindcss-animate";

const config: Config = {
  content: [
    "./node_modules/flowbite-react/**/*.js",
    "./src/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        // nb-gray is wired to CSS variables (RGB channels) defined in
        // src/app/globals.css `:root` (light mode) and `.dark` (dark
        // mode). Same Tailwind class name — `bg-nb-gray-950`,
        // `text-nb-gray-300`, etc — auto-flips to the right value
        // based on the html class. This is what makes the 166
        // components that hardcode `bg-nb-gray-*` work in both themes
        // without a per-file audit.
        //
        // Dark mode anchors at `--oz-ink` (#0f0a1f) for 950 and the
        // violet-shifted scale up to a near-white #f1f1f4 at 50.
        // Light mode mirrors that scale: 950 becomes the lightest
        // surface (page bg) and 50 becomes the deepest text/border.
        // The `<alpha-value>` token preserves Tailwind's opacity
        // suffix support — `bg-nb-gray-950/50` still works.
        "nb-gray": {
          DEFAULT: "rgb(var(--nb-gray-DEFAULT) / <alpha-value>)",
          "50":  "rgb(var(--nb-gray-50)  / <alpha-value>)",
          "100": "rgb(var(--nb-gray-100) / <alpha-value>)",
          "200": "rgb(var(--nb-gray-200) / <alpha-value>)",
          "250": "rgb(var(--nb-gray-250) / <alpha-value>)",
          "300": "rgb(var(--nb-gray-300) / <alpha-value>)",
          "350": "rgb(var(--nb-gray-350) / <alpha-value>)",
          "400": "rgb(var(--nb-gray-400) / <alpha-value>)",
          "500": "rgb(var(--nb-gray-500) / <alpha-value>)",
          "600": "rgb(var(--nb-gray-600) / <alpha-value>)",
          "700": "rgb(var(--nb-gray-700) / <alpha-value>)",
          "800": "rgb(var(--nb-gray-800) / <alpha-value>)",
          "850": "rgb(var(--nb-gray-850) / <alpha-value>)",
          "900": "rgb(var(--nb-gray-900) / <alpha-value>)",
          "910": "rgb(var(--nb-gray-910) / <alpha-value>)",
          "920": "rgb(var(--nb-gray-920) / <alpha-value>)",
          "925": "rgb(var(--nb-gray-925) / <alpha-value>)",
          "930": "rgb(var(--nb-gray-930) / <alpha-value>)",
          "940": "rgb(var(--nb-gray-940) / <alpha-value>)",
          "950": "rgb(var(--nb-gray-950) / <alpha-value>)",
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
  plugins: [flowbitePlugin, tailwindcssAnimate],
};
export default config;
