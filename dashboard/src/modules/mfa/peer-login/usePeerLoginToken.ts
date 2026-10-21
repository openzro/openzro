import { useEffect, useState } from "react";

// usePeerLoginToken reads the approval token from the link's fragment
// (#t=...) and removes it from the address bar, so it does not linger in
// history or get copied along with the URL. The fragment never reaches a
// server. The token is kept in memory only: a reload needs the link again.
// A new link opened in the same tab only changes the fragment, so the hook
// also reads it on hashchange. Every fragment replaces the token, and one
// without t clears it, so the page fails closed instead of acting on an
// earlier link.
// undefined while not read yet; "" when the link carried none.
export default function usePeerLoginToken(): string | undefined {
  const [token, setToken] = useState<string | undefined>(undefined);
  useEffect(() => {
    const read = () => {
      const params = new URLSearchParams(
        window.location.hash.replace(/^#/, ""),
      );
      setToken(params.get("t") ?? "");
      if (window.location.hash) {
        window.history.replaceState(null, "", window.location.pathname);
      }
    };
    read();
    window.addEventListener("hashchange", read);
    return () => window.removeEventListener("hashchange", read);
  }, []);
  return token;
}
