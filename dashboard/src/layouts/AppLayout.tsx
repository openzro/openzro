"use client";

import "../app/globals.css";
import { DisableDarkReader } from "@components/DisableDarkReader";
import { TooltipProvider } from "@components/Tooltip";
import { cn } from "@utils/helpers";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import { Viewport } from "next";
import { Geist, JetBrains_Mono } from "next/font/google";
import localFont from "next/font/local";
import React, { Suspense } from "react";
import { Toaster } from "react-hot-toast";
import OIDCProvider from "@/auth/OIDCProvider";
import FullScreenLoading from "@/components/ui/FullScreenLoading";
import AnalyticsProvider, {
  GoogleTagManagerHeadScript,
} from "@/contexts/AnalyticsProvider";
import DialogProvider from "@/contexts/DialogProvider";
import ErrorBoundaryProvider from "@/contexts/ErrorBoundary";
import { GlobalThemeProvider } from "@/contexts/GlobalThemeProvider";
import { NavigationEvents } from "@/contexts/NavigationEvents";

const inter = localFont({
  src: "../assets/fonts/Inter.ttf",
  display: "swap",
});

// Brand fonts (CLAUDE.md). Wired through CSS variables so the rest of
// the app can pick them up via the tokens defined in globals.css and
// the fontFamily config in tailwind.config.ts.
const geist = Geist({
  subsets: ["latin"],
  variable: "--font-geist",
  weight: ["400", "500", "600", "700", "800"],
  display: "swap",
});

const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-jetbrains-mono",
  weight: ["400", "500", "600"],
  display: "swap",
});

// Extend dayjs with relativeTime plugin
dayjs.extend(relativeTime);

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
};

export default function AppLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" className={cn(geist.variable, jetbrainsMono.variable)}>
      <head>
        <GoogleTagManagerHeadScript />
      </head>
      <body className={cn(inter.className, "dark:bg-nb-gray bg-gray-50")}>
        <Suspense fallback={<FullScreenLoading />}>
          <AnalyticsProvider>
            <DialogProvider>
              <GlobalThemeProvider>
                <ErrorBoundaryProvider>
                  <OIDCProvider>
                    <TooltipProvider delayDuration={0}>
                      {children}
                    </TooltipProvider>
                  </OIDCProvider>
                </ErrorBoundaryProvider>
              </GlobalThemeProvider>
            </DialogProvider>
            <Toaster
              position={"top-center"}
              toastOptions={{
                duration: 3000,
              }}
            />
            <NavigationEvents />
            <DisableDarkReader />
          </AnalyticsProvider>
        </Suspense>
      </body>
    </html>
  );
}
