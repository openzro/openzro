import { useOidc, useOidcUser } from "@axa-fr/react-oidc";
import Button from "@components/Button";
import Paragraph from "@components/Paragraph";
import loadConfig from "@utils/config";
import { ArrowRightIcon } from "lucide-react";
import { useSearchParams } from "next/navigation";
import * as React from "react";
import { useEffect, useState } from "react";
import OpenzroIcon from "@/assets/icons/OpenzroIcon";

const config = loadConfig();

export const OIDCError = () => {
  const { oidcUserLoadingState } = useOidcUser();
  const params = useSearchParams();
  const errorParam = params.get("error");
  const accessDenied = errorParam === "access_denied";
  const invalidRequest = errorParam === "invalid_request";
  const [title, setTitle] = useState(params.get("error_description"));
  const errorDescription = params.get("error_description");
  const { logout } = useOidc();

  useEffect(() => {
    if (accessDenied) {
      if (title === "account linked successfully") {
        setTitle(
          "Your account has been linked successfully. Please log in again to complete the setup.",
        );
      }
    } else {
      setTitle("Oops, something went wrong");
    }
  }, [accessDenied, title]);

  // Scrub the stale code/state the failed token exchange left in the
  // address bar. Without this the Logout button below (and the
  // OidcSecure auto-retry from SecureProvider) re-enters the
  // callback route, axa-fr re-attempts the same dead code, this
  // same error renders, and the user is stuck in a loop that only
  // a manual URL edit breaks. `error` / `error_description` stay
  // because the rendering below reads them.
  //
  // Two URL shapes to handle:
  //   - search-string callbacks   /auth?code=…&state=…
  //   - hash-string callbacks     /#callback?code=…&state=…
  //                               (the loadConfig fallback when the
  //                                deployment didn't set
  //                                AUTH_REDIRECT_URI). The OIDC
  //                                library parses params inside the
  //                                hash too, so leaving them in
  //                                place keeps the loop alive on
  //                                hash-style deployments.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const stale = ["code", "state", "session_state", "iss"];
    let dirty = false;

    const url = new URL(window.location.href);
    for (const key of stale) {
      if (url.searchParams.has(key)) {
        url.searchParams.delete(key);
        dirty = true;
      }
    }

    let hash = url.hash;
    if (hash.startsWith("#")) {
      const qIdx = hash.indexOf("?");
      if (qIdx !== -1) {
        const hashPath = hash.slice(0, qIdx);
        const hashParams = new URLSearchParams(hash.slice(qIdx + 1));
        for (const key of stale) {
          if (hashParams.has(key)) {
            hashParams.delete(key);
            dirty = true;
          }
        }
        const remaining = hashParams.toString();
        hash = remaining ? `${hashPath}?${remaining}` : hashPath;
      }
    }

    if (!dirty) return;
    const rebuilt = url.pathname + url.search + hash;
    window.history.replaceState(null, "", rebuilt);
  }, []);

  return (
    <div
      className={
        "flex items-center justify-center flex-col h-screen max-w-lg mx-auto"
      }
    >
      <div
        className={
          "bg-nb-gray-930 mb-3 border border-nb-gray-900 h-12 w-12 rounded-md flex items-center justify-center "
        }
      >
        <OpenzroIcon size={23} />
      </div>
      <h1 className={"text-center mt-2"}>{title}</h1>

      {accessDenied ? (
        <>
          <Paragraph className={"text-center mt-2"}>
            Already verified your email address?
          </Paragraph>

          <Button
            variant={"primary"}
            size={"sm"}
            className={"mt-5"}
            onClick={() => logout("/", { client_id: config.clientId })}
          >
            Continue
            <ArrowRightIcon size={16} />
          </Button>

          <Button
            variant={"default-outline"}
            size={"sm"}
            className={"mt-5"}
            onClick={() => logout("/", { client_id: config.clientId })}
          >
            Trouble logging in? Try again.
          </Button>
        </>
      ) : (
        <>
          <Paragraph className={"text-center mt-2 block"}>
            There was an error logging you in. <br />
            Error:{" "}
            <span className={"inline capitalize"}>
              {invalidRequest && errorDescription
                ? errorDescription
                : oidcUserLoadingState}
            </span>
          </Paragraph>
          <Button
            variant={"primary"}
            size={"sm"}
            className={"mt-5"}
            onClick={() => logout("/", { client_id: config.clientId })}
          >
            Logout
          </Button>
        </>
      )}
    </div>
  );
};
