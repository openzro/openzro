"use client";

import useSWR from "swr";

// Public GitHub API — 60 req/h per IP unauthenticated, well within
// dashboard usage. SWR caches per-tab so a session typically fires
// one request total. If the dashboard ever needs the data
// pre-rendered or rate-limit-immune, move this behind a management
// proxy at /api/version/release (caches release once per ~hour).
const RELEASES_LATEST =
  "https://api.github.com/repos/openzro/openzro/releases/latest";

export interface ReleaseAsset {
  name: string;
  browser_download_url: string;
  size: number;
}

export interface Release {
  tag_name: string;
  html_url: string;
  published_at: string;
  assets: ReleaseAsset[];
}

const fetcher = async (url: string): Promise<Release> => {
  const res = await fetch(url, {
    headers: { Accept: "application/vnd.github+json" },
  });
  if (!res.ok) {
    throw new Error(`GitHub releases ${res.status}`);
  }
  return res.json();
};

// Latest release of openzro/openzro from the GitHub API.
// Returns the release JSON, an isLoading flag, and any error.
// SWR config: revalidate on focus disabled (release tags don't move
// during a session), 5-minute deduping window so multiple components
// asking in the same render share one fetch.
export function useLatestRelease() {
  return useSWR<Release>(RELEASES_LATEST, fetcher, {
    revalidateOnFocus: false,
    dedupingInterval: 5 * 60 * 1000,
    shouldRetryOnError: false,
  });
}

// Find the asset whose name matches the regex. Returns undefined if
// the release isn't loaded yet or no asset matches — callers should
// fall back to the release page (release.html_url) so the user can
// pick manually if our pattern misses.
export function findAsset(
  release: Release | undefined,
  pattern: RegExp,
): ReleaseAsset | undefined {
  return release?.assets.find((a) => pattern.test(a.name));
}

// Build a fallback URL pointing at the release page on GitHub.
// Used when an asset isn't found (CI hasn't published that variant
// yet, or the API is unreachable).
export function releaseFallbackURL(release: Release | undefined): string {
  return release?.html_url ?? "https://github.com/openzro/openzro/releases/latest";
}
