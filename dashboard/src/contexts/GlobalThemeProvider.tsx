"use client";

import "react-loading-skeleton/dist/skeleton.css";
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
// re-render the SkeletonTheme when next-themes flips.
//
// Dark: base = nb-gray-920 (#252040, slightly above card surface),
//       highlight = nb-gray-800 (#403e60, lighter shimmer).
// Light: base = #e3e3eb, highlight = #f1f1f4 — both lighter than
//       the card surface so placeholders read as a subtle wash, not
//       as competing dark bars on the page (the previous
//       nb-gray-925/930 light values were too saturated).
function ThemedSkeleton({ children }: { children: React.ReactNode }) {
  const { resolvedTheme } = useTheme();
  const isDark = resolvedTheme === "dark";
  return (
    <SkeletonTheme
      baseColor={isDark ? "#252040" : "#e3e3eb"}
      highlightColor={isDark ? "#403e60" : "#f1f1f4"}
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
      <ThemedSkeleton>{children}</ThemedSkeleton>
    </NextThemesProvider>
  );
}
