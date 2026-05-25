"use client";

import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import { setMfaSessionToken } from "@utils/mfaSession";
import { Copy, ShieldCheck } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import React, { useEffect, useState } from "react";
import {
  Modal,
  ModalContent,
  ModalDescription,
  ModalFooter,
  ModalTitle,
} from "@/components/modal/Modal";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";

// MFAEnrollmentModal walks the user through voluntary TOTP enrollment
// in three stages: QR-scan → 6-digit verify → backup-codes display.
// Used from Profile → Security when the user opts in regardless of
// account-level enforcement. The forced-enrollment route at
// /mfa/enroll drives its own flow using the enrollment_token directly
// (separate page, not this modal).

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

interface Props {
  open: boolean;
  onClose: () => void;
  onEnrolled?: () => void;
}

export default function MFAEnrollmentModal({
  open,
  onClose,
  onEnrolled,
}: Readonly<Props>) {
  const startRequest = useApiCall<StartResponse>(
    "/users/me/mfa/enroll/start",
  );
  const finishRequest = useApiCall<FinishResponse>(
    "/users/me/mfa/enroll/finish",
  );

  const [stage, setStage] = useState<Stage>("qr");
  const [otpauthURL, setOtpauthURL] = useState<string>("");
  const [secret, setSecret] = useState<string>("");
  const [pendingToken, setPendingToken] = useState<string>("");
  const [code, setCode] = useState<string>("");
  const [codeError, setCodeError] = useState<string>("");
  const [backupCodes, setBackupCodes] = useState<string[]>([]);
  const [savedConfirmed, setSavedConfirmed] = useState<boolean>(false);
  const [loading, setLoading] = useState<boolean>(false);

  // Reset state when reopening so a previous attempt's QR + code
  // input don't leak into the new flow.
  useEffect(() => {
    if (open) {
      setStage("qr");
      setCode("");
      setCodeError("");
      setBackupCodes([]);
      setSavedConfirmed(false);
      void start();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const start = async () => {
    setLoading(true);
    try {
      const res = await startRequest.post({});
      setOtpauthURL(res.otpauth_url);
      setSecret(res.secret);
      setPendingToken(res.pending_token);
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Enrollment start failed";
      notify({
        title: "Two-factor authentication",
        description: msg,
      });
    } finally {
      setLoading(false);
    }
  };

  const verify = async () => {
    if (code.length !== 6) {
      setCodeError("Enter the 6-digit code from your authenticator app.");
      return;
    }
    if (!pendingToken) {
      setCodeError("Enrollment session expired — close and restart.");
      return;
    }
    setLoading(true);
    setCodeError("");
    try {
      const res = await finishRequest.post({ code, pending_token: pendingToken });
      setBackupCodes(res.backup_codes);
      // Capture the mfa_session_token so subsequent gated requests
      // (regenerate, disable) ride a verified session without a
      // fresh challenge.
      if (res.mfa_session_token) {
        setMfaSessionToken(res.mfa_session_token);
      }
      setStage("backup-codes");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Verification failed";
      setCodeError(msg);
    } finally {
      setLoading(false);
    }
  };

  const finishUp = () => {
    onEnrolled?.();
    onClose();
  };

  return (
    <Modal open={open} onOpenChange={(o) => !o && onClose()}>
      <ModalContent maxWidthClass="max-w-[480px]">
        {stage === "qr" && (
          <>
            {/* px-8 mirrors ModalFooter's horizontal padding so the
                title, description, and QR block don't sit flush
                against the modal's borders. ModalContent itself
                intentionally has no horizontal padding so every
                modal can pick its own — see DnsZoneModal /
                NameserverModal for the same convention. */}
            <div className="px-8 pb-6">
              <ModalTitle>Set up two-factor authentication</ModalTitle>
              <ModalDescription className="mt-2">
                Scan the QR code with your authenticator app (Google
                Authenticator, Authy, 1Password, Bitwarden). The app
                generates a fresh 6-digit code every 30 seconds.
              </ModalDescription>
              <div className="mt-4 flex flex-col items-center gap-4">
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
            </div>
            <ModalFooter>
              <OzButton variant="ghost" type="button" onClick={onClose}>
                Cancel
              </OzButton>
              <OzButton
                variant="primary"
                type="button"
                disabled={!otpauthURL || loading}
                onClick={() => setStage("verify")}
              >
                I scanned it — next
              </OzButton>
            </ModalFooter>
          </>
        )}

        {stage === "verify" && (
          <>
            <div className="px-8 pb-6">
              <ModalTitle>Verify your authenticator</ModalTitle>
              <ModalDescription className="mt-2">
                Enter the 6-digit code from your authenticator app to
                confirm enrollment.
              </ModalDescription>
              <div className="mt-4">
                <OzInput
                  value={code}
                  onChange={(e) =>
                    setCode(e.target.value.replace(/[^0-9]/g, "").slice(0, 6))
                  }
                  placeholder="123456"
                  inputMode="numeric"
                  pattern="[0-9]{6}"
                  maxLength={6}
                  error={codeError}
                  autoFocus
                  data-cy="mfa-verify-code"
                />
              </div>
            </div>
            <ModalFooter>
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
              >
                Verify
              </OzButton>
            </ModalFooter>
          </>
        )}

        {stage === "backup-codes" && (
          <>
            <div className="px-8 pb-6">
              <ModalTitle className="flex items-center gap-2">
                <ShieldCheck size={18} className="text-emerald-500" />
                Save your backup codes
              </ModalTitle>
              <ModalDescription className="mt-2">
                Store these 10 single-use codes somewhere safe — a password
                manager, a printout in a locked drawer. Each code works
                once, in place of a TOTP code, if you lose access to your
                authenticator app. <strong>You won&apos;t see them again.</strong>
              </ModalDescription>
              <div className="mt-4 grid grid-cols-2 gap-2 rounded-md bg-oz2-bg-sunken p-4 font-mono text-[13px]">
                {backupCodes.map((c) => (
                  <div key={c} className="text-oz2-text">
                    {c}
                  </div>
                ))}
              </div>
              <button
                type="button"
                className="mt-2 inline-flex items-center gap-1 text-[12px] text-oz2-text-muted hover:text-oz2-text"
                onClick={() =>
                  navigator.clipboard.writeText(backupCodes.join("\n"))
                }
              >
                <Copy size={12} /> Copy all
              </button>
              <label className="mt-4 flex items-start gap-2 text-[13px] text-oz2-text-muted">
                <input
                  type="checkbox"
                  checked={savedConfirmed}
                  onChange={(e) => setSavedConfirmed(e.target.checked)}
                  className="mt-1"
                />
                <span>I&apos;ve saved these codes somewhere safe.</span>
              </label>
            </div>
            <ModalFooter>
              <OzButton
                variant="primary"
                type="button"
                disabled={!savedConfirmed}
                onClick={finishUp}
              >
                Done
              </OzButton>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}
