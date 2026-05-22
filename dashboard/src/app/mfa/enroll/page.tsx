"use client";

import loadConfig from "@utils/config";
import {
  clearMfaRedirectToken,
  getMfaRedirectToken,
  setMfaSessionToken,
} from "@utils/mfaSession";
import { Copy, ShieldCheck } from "lucide-react";
import { useRouter } from "next/navigation";
import { QRCodeSVG } from "qrcode.react";
import React, { useEffect, useMemo, useState } from "react";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import OzInput from "@/components/v2/OzInput";

// /mfa/enroll — interstitial the auth middleware redirects to when
// the operator has enabled MFAEnforceLocal / MFAEnforceFederated and
// the user has no enrolled TOTP yet. Drives the same QR → verify →
// backup-codes UX as the in-profile modal, but with the
// enrollment_token in the Authorization header instead of the user's
// Dex JWT (the dashboard would otherwise fail the gate before
// reaching here).

type Stage = "qr" | "verify" | "backup-codes";

interface StartResponse {
  otpauth_url: string;
  secret: string;
  pending_token: string;
}

interface FinishResponse {
  backup_codes: string[];
  mfa_session_token: string;
}

export default function MFAForcedEnrollPage() {
  const router = useRouter();
  const config = useMemo(() => loadConfig(), []);
  const [token, setToken] = useState<string | undefined>(undefined);
  const [stage, setStage] = useState<Stage>("qr");
  const [otpauthURL, setOtpauthURL] = useState("");
  const [secret, setSecret] = useState("");
  const [pendingToken, setPendingToken] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [backupCodes, setBackupCodes] = useState<string[]>([]);
  const [savedConfirmed, setSavedConfirmed] = useState(false);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    setToken(getMfaRedirectToken());
  }, []);

  useEffect(() => {
    if (!token) return;
    void start(token);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  const start = async (t: string) => {
    setLoading(true);
    try {
      const res = await fetch(`${config.apiOrigin}/api/mfa/enroll/start`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${t}`,
        },
        body: "{}",
      });
      if (!res.ok) {
        setError("Could not start enrollment. Sign in again.");
        return;
      }
      const body = (await res.json()) as StartResponse;
      setOtpauthURL(body.otpauth_url);
      setSecret(body.secret);
      setPendingToken(body.pending_token);
    } catch {
      setError("Network error.");
    } finally {
      setLoading(false);
    }
  };

  const verify = async () => {
    if (!token || !pendingToken) {
      setError("Enrollment session expired — sign in again.");
      return;
    }
    if (code.length !== 6) {
      setError("Enter the 6-digit code from your authenticator app.");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const res = await fetch(`${config.apiOrigin}/api/mfa/enroll/finish`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ code, pending_token: pendingToken }),
      });
      if (!res.ok) {
        setError("Code does not match. Try again.");
        return;
      }
      const body = (await res.json()) as FinishResponse;
      setBackupCodes(body.backup_codes);
      if (body.mfa_session_token) {
        setMfaSessionToken(body.mfa_session_token);
      }
      setStage("backup-codes");
    } catch {
      setError("Network error.");
    } finally {
      setLoading(false);
    }
  };

  const finish = () => {
    clearMfaRedirectToken();
    router.replace("/");
  };

  if (token === undefined) return null;

  if (!token) {
    return (
      <OzCard className="w-full space-y-4">
        <div className="text-[14px] font-semibold text-oz2-text">
          MFA enrollment required
        </div>
        <p className="text-[13px] text-oz2-text-muted">
          No enrollment token in this session. Sign in again to receive a
          fresh enrollment link.
        </p>
        <OzButton variant="primary" type="button" onClick={() => router.replace("/")}>
          Return to sign-in
        </OzButton>
      </OzCard>
    );
  }

  return (
    <OzCard className="w-full space-y-4">
      {stage === "qr" && (
        <>
          <div className="text-[16px] font-semibold text-oz2-text">
            Set up two-factor authentication
          </div>
          <p className="text-[13px] text-oz2-text-muted">
            Your operator requires a second factor on this account. Scan
            the QR code with an authenticator app (Google Authenticator,
            Authy, 1Password, Bitwarden), then enter the 6-digit code.
          </p>
          <div className="flex flex-col items-center gap-4">
            {otpauthURL ? (
              <div className="rounded-lg bg-white p-4">
                <QRCodeSVG value={otpauthURL} size={192} level="M" />
              </div>
            ) : (
              <div className="h-[224px] w-[224px] animate-pulse rounded-lg bg-oz2-bg-sunken" />
            )}
            {secret && (
              <details className="w-full text-[12px] text-oz2-text-muted">
                <summary className="cursor-pointer select-none">
                  Can&apos;t scan? Enter this code manually.
                </summary>
                <div className="mt-2 flex items-center gap-2 rounded-md bg-oz2-bg-sunken p-3 font-mono text-[13px] text-oz2-text">
                  <span className="flex-1 break-all">{secret}</span>
                  <button
                    type="button"
                    className="rounded-md p-1 hover:bg-oz2-bg-soft"
                    onClick={() => navigator.clipboard.writeText(secret)}
                    title="Copy secret"
                  >
                    <Copy size={14} />
                  </button>
                </div>
              </details>
            )}
          </div>
          <OzButton
            variant="primary"
            type="button"
            disabled={!otpauthURL || loading}
            onClick={() => setStage("verify")}
          >
            I scanned it — next
          </OzButton>
          {error && <div className="text-[13px] text-rose-500">{error}</div>}
        </>
      )}

      {stage === "verify" && (
        <>
          <div className="text-[16px] font-semibold text-oz2-text">
            Verify your authenticator
          </div>
          <p className="text-[13px] text-oz2-text-muted">
            Enter the 6-digit code from your authenticator app to complete
            enrollment.
          </p>
          <OzInput
            value={code}
            onChange={(e) =>
              setCode(e.target.value.replace(/[^0-9]/g, "").slice(0, 6))
            }
            placeholder="123456"
            inputMode="numeric"
            pattern="[0-9]{6}"
            maxLength={6}
            error={error}
            autoFocus
            data-cy="mfa-forced-verify-code"
          />
          <div className="flex gap-2">
            <OzButton
              variant="ghost"
              type="button"
              onClick={() => setStage("qr")}
            >
              Back
            </OzButton>
            <OzButton
              variant="primary"
              type="button"
              disabled={loading || code.length !== 6}
              onClick={verify}
              data-cy="mfa-forced-verify-submit"
            >
              {loading ? "Verifying…" : "Verify"}
            </OzButton>
          </div>
        </>
      )}

      {stage === "backup-codes" && (
        <>
          <div className="flex items-center gap-2 text-[16px] font-semibold text-oz2-text">
            <ShieldCheck size={20} className="text-emerald-500" />
            Save your backup codes
          </div>
          <p className="text-[13px] text-oz2-text-muted">
            Store these 10 single-use codes somewhere safe — a password
            manager, a printout in a locked drawer. Each code works once,
            in place of a TOTP code, if you lose access to your
            authenticator app.{" "}
            <strong>You won&apos;t see them again.</strong>
          </p>
          <div className="grid grid-cols-2 gap-2 rounded-md bg-oz2-bg-sunken p-4 font-mono text-[13px]">
            {backupCodes.map((c) => (
              <div key={c} className="text-oz2-text">
                {c}
              </div>
            ))}
          </div>
          <button
            type="button"
            className="inline-flex items-center gap-1 text-[12px] text-oz2-text-muted hover:text-oz2-text"
            onClick={() =>
              navigator.clipboard.writeText(backupCodes.join("\n"))
            }
          >
            <Copy size={12} /> Copy all
          </button>
          <label className="mt-2 flex items-start gap-2 text-[13px] text-oz2-text-muted">
            <input
              type="checkbox"
              checked={savedConfirmed}
              onChange={(e) => setSavedConfirmed(e.target.checked)}
              className="mt-1"
            />
            <span>I&apos;ve saved these codes somewhere safe.</span>
          </label>
          <OzButton
            variant="primary"
            type="button"
            disabled={!savedConfirmed}
            onClick={finish}
          >
            Done — continue to dashboard
          </OzButton>
        </>
      )}
    </OzCard>
  );
}
