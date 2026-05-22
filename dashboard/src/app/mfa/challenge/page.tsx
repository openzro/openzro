"use client";

import loadConfig from "@utils/config";
import {
  clearMfaRedirectToken,
  getMfaRedirectToken,
  setMfaSessionToken,
} from "@utils/mfaSession";
import { ShieldCheck, ShieldQuestion } from "lucide-react";
import { useRouter } from "next/navigation";
import React, { useEffect, useMemo, useState } from "react";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import OzInput from "@/components/v2/OzInput";

// /mfa/challenge — interstitial the auth middleware redirects to when
// an enforced + enrolled user lacks a valid mfa_session_token. Reads
// the one-shot challenge_token the api.tsx interceptor stashed in
// sessionStorage, calls POST /api/mfa/challenge with the user's
// 6-digit code, captures the returned mfa_session_token, and bounces
// back to the originating dashboard route.
//
// Why a bare fetch + manual Authorization header rather than
// useApiCall: this endpoint is JWT-less (the gate-emitted
// challenge_token IS the auth) and the rest of the api.tsx flow
// assumes a Dex JWT in the bearer slot — wiring useApiCall to a
// different token would leak the abstraction.

interface ChallengeResponse {
  ok: boolean;
  used_backup_code?: boolean;
  locked?: boolean;
  locked_until?: string;
  mfa_session_token?: string;
}

export default function MFAChallengePage() {
  const router = useRouter();
  const config = useMemo(() => loadConfig(), []);
  const [token, setToken] = useState<string | undefined>(undefined);
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    const t = getMfaRedirectToken();
    setToken(t);
  }, []);

  const submit = async () => {
    if (!token) {
      setError("No challenge token — return to sign-in.");
      return;
    }
    if (code.length < 6) {
      setError("Enter the 6-digit code from your authenticator app.");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const res = await fetch(`${config.apiOrigin}/api/mfa/challenge`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ code }),
      });
      const body = (await res.json()) as ChallengeResponse;
      if (!res.ok) {
        setError("Verification failed. Try again.");
        return;
      }
      if (body.locked) {
        setError(
          body.locked_until
            ? `Locked until ${new Date(body.locked_until).toLocaleTimeString()} after too many failed attempts.`
            : "Locked after too many failed attempts.",
        );
        return;
      }
      if (!body.ok) {
        setError("Code does not match. Try the next one from your app.");
        return;
      }
      if (body.mfa_session_token) {
        setMfaSessionToken(body.mfa_session_token);
      }
      clearMfaRedirectToken();
      // Bounce home — the dashboard layout's next request rides the
      // fresh X-MFA-Token and the gate passes.
      router.replace("/");
    } catch (e) {
      setError("Network error. Try again.");
    } finally {
      setLoading(false);
    }
  };

  if (token === undefined) {
    return null;
  }

  if (!token) {
    return (
      <OzCard className="w-full space-y-4">
        <div className="flex items-center gap-2 text-[14px] font-semibold text-oz2-text">
          <ShieldQuestion size={18} className="text-amber-500" />
          MFA verification required
        </div>
        <p className="text-[13px] text-oz2-text-muted">
          No challenge token in this session. Sign in again to receive a
          fresh challenge.
        </p>
        <OzButton variant="primary" type="button" onClick={() => router.replace("/")}>
          Return to sign-in
        </OzButton>
      </OzCard>
    );
  }

  return (
    <OzCard className="w-full space-y-4">
      <div className="flex items-center gap-2 text-[16px] font-semibold text-oz2-text">
        <ShieldCheck size={20} className="text-oz2-acc" />
        Verify your second factor
      </div>
      <p className="text-[13px] text-oz2-text-muted">
        Enter the 6-digit code from your authenticator app to continue.
        Out of codes? Type one of your backup codes (with or without
        dashes) instead.
      </p>
      <OzInput
        value={code}
        onChange={(e) =>
          setCode(e.target.value.replace(/[^0-9a-fA-F]/g, "").slice(0, 32))
        }
        placeholder="123456"
        inputMode="text"
        autoFocus
        error={error}
        data-cy="mfa-challenge-code"
      />
      <OzButton
        variant="primary"
        type="button"
        onClick={submit}
        disabled={loading || code.length < 6}
        data-cy="mfa-challenge-submit"
      >
        {loading ? "Verifying…" : "Verify"}
      </OzButton>
    </OzCard>
  );
}
