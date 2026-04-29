"use client";

import * as React from "react";

// Third-party tracker scripts (Google Analytics, Google Tag Manager,
// Hotjar) shipped with the upstream NetBird dashboard were removed
// from the openZro fork — they're inappropriate for a self-hostable
// zero-trust networking project, and the runtime env-var
// substitution that gated them was unreliable (operators saw literal
// `$OPENZRO_HOTJAR_TRACK_ID` strings hit the network as 404s and
// CORS errors).
//
// This file remains as a thin no-op shim so the existing call sites
// (`NavigationEvents`, components that call `trackEvent` etc.) keep
// compiling without churn. The whole module can be deleted in a
// follow-up sweep along with the call sites once we're sure no
// downstream behaviour depended on the side effects.

type Props = {
  children: React.ReactNode;
};

const noop = () => {};

const AnalyticsContext = React.createContext({
  initialized: false,
  trackPageView: noop,
  trackEvent: (_category: string, _action: string, _label: string) => {},
  trackEventV2: (
    _category: string,
    _name: string,
    _value?: string,
    _userID?: string,
  ) => {},
  trackGTMCustomEvent: (_name: string) => {},
});

export default function AnalyticsProvider({ children }: Readonly<Props>) {
  return (
    <AnalyticsContext.Provider
      value={{
        initialized: false,
        trackPageView: noop,
        trackEvent: noop,
        trackEventV2: noop,
        trackGTMCustomEvent: noop,
      }}
    >
      {children}
    </AnalyticsContext.Provider>
  );
}

// Kept as a named export so AppLayout.tsx (which still imports it)
// keeps compiling. Returns null — no tracker scripts injected.
export const GoogleTagManagerHeadScript = () => null;

export const useAnalytics = () => React.useContext(AnalyticsContext);
