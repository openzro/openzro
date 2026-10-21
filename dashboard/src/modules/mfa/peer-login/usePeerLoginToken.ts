import { useEffect, useState } from "react";

// usePeerLoginToken reads the approval token from the link's fragment
// (#t=...) and removes it from the address bar, so it does not linger in
// history or get copied along with the URL. The fragment never reaches a
// server. The token is kept in memory only: a reload needs the link again.
// A new link opened in the same tab only changes the fragment, so the hook
// also picks the token up on hashchange.
// undefined while not read yet; "" when the link carried none.
export default function usePeerLoginToken(): string | undefined {
  const [token, setToken] = useState<string | undefined>(undefined);
  useEffect(() => {
    const read = (initial: boolean) => {
      const params = new URLSearchParams(
        window.location.hash.replace(/^#/, ""),
      );
      const t = params.get("t");
      if (t !== null || initial) setToken(t ?? "");
      if (window.location.hash) {
        window.history.replaceState(null, "", window.location.pathname);
      }
    };
    read(true);
    const onHashChange = () => read(false);
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);
  return token;
}
