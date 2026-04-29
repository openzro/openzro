"use client";

import "react-loading-skeleton/dist/skeleton.css";
import { openzroTheme } from "@utils/theme";
import { Flowbite } from "flowbite-react";
import dynamic from "next/dynamic";
import { useTheme } from "next-themes";
import { type ThemeProviderProps } from "next-themes/dist/types";
import * as React from "react";
import { SkeletonTheme } from "react-loading-skeleton";

const NextThemesProvider = dynamic(
  () => import("next-themes").then((mod) => mod.ThemeProvider),
  { ssr: false },
);

// Skeleton (react-loading-skeleton) doesn't read CSS variables — it
// takes hex strings at provider level and bakes them into inline
// styles. So we resolve the right pair based on the active theme and
// re-render the SkeletonTheme when next-themes flips. Tied to the
// nb-gray scale: base = -920 (card surface), highlight = -800
// (slightly lighter shimmer), values mirrored between light and dark
// in src/app/globals.css.
function ThemedSkeleton({ children }: { children: React.ReactNode }) {
  const { resolvedTheme } = useTheme();
  const isDark = resolvedTheme === "dark";
  return (
    <SkeletonTheme
      baseColor={isDark ? "#252040" : "#c1c1d0"}
      highlightColor={isDark ? "#403e60" : "#d0d0db"}
    >
      {children}
    </SkeletonTheme>
  );
}

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
        <ThemedSkeleton>{children}</ThemedSkeleton>
      </Flowbite>
    </NextThemesProvider>
  );
}
