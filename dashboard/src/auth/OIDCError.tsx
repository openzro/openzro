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
  const { logout, login } = useOidc();

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

  // Scrub the stale ?code= / ?state= that the failed token exchange
  // left in the address bar. Without this the Logout button below
  // (and the OidcSecure auto-retry from SecureProvider) re-enters
  // /auth?code=..., axa-fr re-attempts the same dead code, this same
  // error renders, and the user is stuck in a loop that only a manual
  // URL edit breaks. We keep `error` / `error_description` because
  // the rendering below reads them.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (!url.searchParams.has("code") && !url.searchParams.has("state")) {
      return;
    }
    url.searchParams.delete("code");
    url.searchParams.delete("state");
    url.searchParams.delete("session_state");
    url.searchParams.delete("iss");
    window.history.replaceState(null, "", url.pathname + url.search);
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
