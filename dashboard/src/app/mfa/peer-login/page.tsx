"use client";

import loadConfig from "@utils/config";
import dayjs from "dayjs";
import { ShieldCheck } from "lucide-react";
import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import OzInput from "@/components/v2/OzInput";
import { useDialog } from "@/contexts/DialogProvider";
import {
  approveWithCode,
  denyApproval,
  loadApproval,
  LoadResult,
  PeerLoginApproval,
} from "@/modules/mfa/peer-login/api";
import PeerLoginDeviceCard from "@/modules/mfa/peer-login/PeerLoginDeviceCard";
import {
  PeerLoginNotice,
  type PeerLoginNoticeKind,
  PeerLoginOutcome,
} from "@/modules/mfa/peer-login/PeerLoginOutcome";
import usePeerLoginToken from "@/modules/mfa/peer-login/usePeerLoginToken";

// /mfa/peer-login — the device-login approval page (ADR-0028 D1, D4). The
// openZro app opens it when a device sign-in needs the user's second
// factor. It works without a dashboard session: the link's token is the
// credential. The user checks the device and approves with a code, or
// answers "Not me".

type Page =
  | { kind: "loading" }
  // The approval shown is tied to the token it was loaded with, so the
  // buttons always act on the attempt on screen.
  | { kind: "loaded"; token: string; approval: PeerLoginApproval }
  | { kind: PeerLoginNoticeKind };

function fromLoad(token: string, result: LoadResult): Page {
  return result.kind === "ok"
    ? { kind: "loaded", token, approval: result.approval }
    : result;
}

export default function PeerLoginApprovalPage() {
  const config = useMemo(() => loadConfig(), []);
  const token = usePeerLoginToken();
  const { confirm } = useDialog();
  const [page, setPage] = useState<Page>({ kind: "loading" });
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  // generation counts the links seen; a response for an older one is
  // dropped, so it cannot overwrite the page of a newer link. inFlight
  // blocks a second submit synchronously (busy only applies on the next
  // render), so a held Enter key cannot send several codes at once.
  const generation = useRef(0);
  const inFlight = useRef(false);

  const load = useCallback(
    async (tok: string, gen: number) => {
      const result = await loadApproval(config.apiOrigin, tok);
      if (gen === generation.current) setPage(fromLoad(tok, result));
    },
    [config.apiOrigin],
  );

  useEffect(() => {
    if (token === undefined) return;
    const gen = ++generation.current;
    // A new link may arrive in the same tab: start from a clean page.
    inFlight.current = false;
    setBusy(false);
    setCode("");
    setError("");
    if (!token) {
      setPage({ kind: "no_token" });
      return;
    }
    setPage({ kind: "loading" });
    void load(token, gen);
  }, [token, load]);

  const approve = async () => {
    if (page.kind !== "loaded" || inFlight.current || code.length < 6) return;
    const tok = page.token;
    const gen = generation.current;
    inFlight.current = true;
    setBusy(true);
    setError("");
    const result = await approveWithCode(config.apiOrigin, tok, code);
    if (gen !== generation.current) return;
    inFlight.current = false;
    setBusy(false);
    switch (result.kind) {
      case "approved":
      case "gone":
        setCode("");
        return load(tok, gen);
      case "wrong_code":
        return setError(
          "That code does not match. Check your authenticator app.",
        );
      case "code_reused":
        return setError(
          "That code was already used. Wait for the next one from your app.",
        );
      case "locked":
        return setError(
          result.lockedUntil
            ? `Too many wrong codes. Try again after ${dayjs(
                result.lockedUntil,
              ).format("HH:mm")}.`
            : "Too many wrong codes. Try again later.",
        );
      case "invalid":
        return setPage({ kind: "invalid" });
      default:
        return setError("Something went wrong. Try again.");
    }
  };

  const deny = async () => {
    if (page.kind !== "loaded" || inFlight.current) return;
    const tok = page.token;
    const gen = generation.current;
    const sure = await confirm({
      title: "Deny this sign-in?",
      description:
        "The device will not be able to connect with this attempt. If it was you after all, start again from the openZro app once this request expires.",
      confirmText: "Deny sign-in",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!sure || gen !== generation.current || inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    const result = await denyApproval(config.apiOrigin, tok);
    if (gen !== generation.current) return;
    inFlight.current = false;
    setBusy(false);
    if (result.kind === "ok" || result.kind === "gone") return load(tok, gen);
    setPage(fromLoad(tok, result));
  };

  if (page.kind === "loading") return null;
  if (page.kind !== "loaded") return <PeerLoginNotice kind={page.kind} />;

  const { approval } = page;
  if (approval.state !== "pending")
    return <PeerLoginOutcome approval={approval} />;

  return (
    <OzCard className="w-full space-y-4" data-cy="peer-login-pending">
      <div className="flex items-center gap-2 text-[16px] font-semibold text-oz2-text">
        <ShieldCheck size={20} className="text-oz2-acc" />
        Approve a device sign-in
      </div>
      <p className="text-[13px] text-oz2-text-muted">
        A device is signing in to openZro as you. If it is yours, enter the
        6-digit code from your authenticator app, or a backup code. If you did
        not start this, choose <strong>Not me</strong>.
      </p>
      <PeerLoginDeviceCard approval={approval} />
      <OzInput
        value={code}
        onChange={(e) =>
          setCode(e.target.value.replace(/[^0-9a-fA-F]/g, "").slice(0, 32))
        }
        onKeyDown={(e) => e.key === "Enter" && void approve()}
        placeholder="123456"
        inputMode="text"
        autoComplete="one-time-code"
        autoFocus
        error={error}
        data-cy="peer-login-code"
      />
      <div className="flex gap-2">
        <OzButton
          variant="primary"
          type="button"
          className="flex-1"
          onClick={() => void approve()}
          disabled={busy || code.length < 6}
          data-cy="peer-login-approve"
        >
          {busy ? "Checking…" : "Approve"}
        </OzButton>
        <OzButton
          type="button"
          onClick={() => void deny()}
          disabled={busy}
          data-cy="peer-login-deny"
        >
          Not me
        </OzButton>
      </div>
      <p className="text-[12px] text-oz2-text-faint">
        This request expires at {dayjs(approval.expires_at).format("HH:mm")}.
      </p>
    </OzCard>
  );
}
