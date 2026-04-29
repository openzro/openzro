"use client";

import "react-loading-skeleton/dist/skeleton.css";
import { openzroTheme } from "@utils/theme";
import { Flowbite } from "flowbite-react";
import dynamic from "next/dynamic";
import { type ThemeProviderProps } from "next-themes/dist/types";
import * as React from "react";
import { SkeletonTheme } from "react-loading-skeleton";

const NextThemesProvider = dynamic(
  () => import("next-themes").then((mod) => mod.ThemeProvider),
  { ssr: false },
);

export function GlobalThemeProvider({
  children,
  ...props
}: ThemeProviderProps) {
  return (
    <NextThemesProvider
      attribute="class"
      defaultTheme="dark"
      storageKey="openzro-theme"
      enableSystem={true}
      disableTransitionOnChange
      {...props}
    >
      <Flowbite theme={{ theme: openzroTheme }}>
        {/* Skeleton placeholder colors aligned with the violet-shifted
            nb-gray palette — base = nb-gray-920 (#252040, card surface),
            highlight = nb-gray-800 (#403e60, slightly lighter shimmer).
            Matches the brand-aware dark theme; previously these were
            hardcoded to the neutral gray scale and visually clashed
            with the rest of the dashboard after the palette shift. */}
        <SkeletonTheme baseColor={"#252040"} highlightColor={"#403e60"}>
          {children}
        </SkeletonTheme>
      </Flowbite>
    </NextThemesProvider>
  );
}
