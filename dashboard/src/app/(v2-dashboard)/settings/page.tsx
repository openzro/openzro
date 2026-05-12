"use client";

import FullScreenLoading from "@components/ui/FullScreenLoading";
import { useRouter } from "next/navigation";
import { useEffect } from "react";

// Bare /settings — redirects into the first sub-tab so the sidebar
// item (which points at /settings) lands on an actual screen. The
// segmented SettingsTabsV2 sub-nav inside each sub-page exposes the
// other sections without another sidebar round-trip.

export default function Settings() {
  const router = useRouter();

  useEffect(() => {
    router.push("/settings/authentication");
  }, [router]);

  return <FullScreenLoading height={"auto"} />;
}
