"use client";

import useSWR from "swr";

// pkg.openzro.io/latest.json — tiny `{tag, version, updated}` blob
// published by scripts/publish-packages.sh on every tag push. Same
// origin as the .msi / .pkg download URLs the WindowsTab and
// MacOSTab link to, so no extra DNS lookup, no GitHub API rate
// limit (60 req/h unauthenticated), no cross-origin headers needed.
const LATEST_JSON = "https://pkg.openzro.io/latest.json";

export interface Release {
  tag: string;
  version: string;
  updated: string;
  // Compatibility alias for callers that still read tag_name.
  tag_name: string;
}

const fetcher = async (url: string): Promise<Release> => {
  const res = await fetch(url, { headers: { Accept: "application/json" } });
  if (!res.ok) {
    throw new Error(`pkg.openzro.io/latest.json ${res.status}`);
  }
  const j = await res.json();
  return { ...j, tag_name: j.tag };
};

// Latest published openZro release. Used by the Setup modal tabs to
// label the download buttons with the version string. The download
// URLs themselves are version-independent
// (https://pkg.openzro.io/{windows,macos}/openzro.{msi,pkg}) so the
// dashboard works even if this fetch fails.
export function useLatestRelease() {
  return useSWR<Release>(LATEST_JSON, fetcher, {
    revalidateOnFocus: false,
    dedupingInterval: 5 * 60 * 1000,
    shouldRetryOnError: false,
  });
}
