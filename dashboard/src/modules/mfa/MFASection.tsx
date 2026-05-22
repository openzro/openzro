"use client";

import { notify } from "@components/Notification";
import useFetchApi, { useApiCall } from "@utils/api";
import { clearMfaSessionToken } from "@utils/mfaSession";
import dayjs from "dayjs";
import { ShieldCheck, ShieldOff, ShieldQuestion } from "lucide-react";
import React, { useState } from "react";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import MFAEnrollmentModal from "@/modules/mfa/MFAEnrollmentModal";

// MFASection — the user's OWN second-factor enrollment surface, rendered
// as a card on their profile page (/team/user?id=<own-id>). Operator-wide
// MFA enforcement lives on Settings → Authentication; this card is
// strictly per-user and only renders for the logged-in user looking at
// their own profile.
//
// State machine:
//   - GET /api/users/me/mfa returns enrolled bool + counters.
//   - Not enrolled → "Set up 2FA" → enrollment modal.
//   - Enrolled    → status card + Regenerate / Disable actions.

interface MFAStatus {
  Enrolled: boolean;
  EnrolledAt?: string | null;
  LastVerifiedAt?: string | null;
  BackupCodesRemaining: number;
  Locked: boolean;
  LockedUntil?: string | null;
}

interface RegenerateResponse {
  backup_codes: string[];
}

export default function MFASection() {
  // Use the project-wide useFetchApi helper rather than raw useSWR —
  // there is no global SWR fetcher / SWRConfig configured, so raw
  // useSWR would just store the URL as a cache key and never fetch.
  const { data, mutate, isLoading } = useFetchApi<MFAStatus>("/users/me/mfa");
  const [enrollOpen, setEnrollOpen] = useState(false);

  const disenroll = useApiCall<{ disenrolled: boolean }>(
    "/users/me/mfa",
  );
  const regenerate = useApiCall<RegenerateResponse>(
    "/users/me/mfa/backup-codes/regenerate",
  );

  const [newCodes, setNewCodes] = useState<string[] | null>(null);

  const handleDisable = async () => {
    if (
      !window.confirm(
        "Disable two-factor authentication? You can re-enrol any time. If your account is under operator-enforced MFA you will be prompted to enrol again on next sign-in.",
      )
    ) {
      return;
    }
    try {
      await disenroll.del();
      // Once MFA is off, the elevated session token is no longer
      // meaningful — clear it so subsequent requests don't carry a
      // stale X-MFA-Token (harmless but pointless).
      clearMfaSessionToken();
      notify({
        title: "Two-factor authentication",
        description: "2FA disabled. Re-enroll any time from this page.",
      });
      void mutate();
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Disable failed";
      notify({
        title: "Two-factor authentication",
        description: msg,
      });
    }
  };

  const handleRegenerate = async () => {
    if (
      !window.confirm(
        "Regenerate backup codes? Any previously generated codes will stop working immediately.",
      )
    ) {
      return;
    }
    try {
      const res = await regenerate.post({});
      setNewCodes(res.backup_codes);
      void mutate();
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Regenerate failed";
      notify({
        title: "Two-factor authentication",
        description: msg,
      });
    }
  };

  return (
    <OzCard className="space-y-4">
      <div>
        <label className="text-[13px] font-semibold text-oz2-text">
          Two-factor authentication
        </label>
        <p className="mt-1 text-[12.5px] text-oz2-text-muted">
          Add a 6-digit TOTP code from an authenticator app (Google
          Authenticator, Authy, 1Password, Bitwarden) on top of your
          openZro sign-in. Operator-wide enforcement is set on{" "}
          <strong>Settings → Authentication</strong>.
        </p>
      </div>

      {isLoading && (
        <div className="text-[13px] text-oz2-text-muted">Loading…</div>
      )}

      {data && !data.Enrolled && (
        <div className="flex flex-col items-start gap-3">
          <div className="flex items-center gap-2 text-[13px] text-oz2-text-muted">
            <ShieldQuestion size={16} className="text-amber-500" />
            <span>Two-factor authentication is not enabled.</span>
          </div>
          <OzButton
            variant="primary"
            type="button"
            onClick={() => setEnrollOpen(true)}
            data-cy="mfa-enroll-start"
          >
            Set up two-factor authentication
          </OzButton>
        </div>
      )}

      {data && data.Enrolled && (
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-2 text-[13px] text-emerald-600 dark:text-emerald-400">
            <ShieldCheck size={16} />
            <span>
              Two-factor authentication is active
              {data.EnrolledAt
                ? ` since ${dayjs(data.EnrolledAt).format("MMM D, YYYY")}`
                : ""}
              .
            </span>
          </div>
          <div className="text-[13px] text-oz2-text-muted">
            <div>
              Backup codes remaining:{" "}
              <strong className="text-oz2-text">
                {data.BackupCodesRemaining}
              </strong>{" "}
              / 10
            </div>
            {data.LastVerifiedAt && (
              <div>
                Last verified:{" "}
                {dayjs(data.LastVerifiedAt).format("MMM D, YYYY HH:mm")}
              </div>
            )}
            {data.Locked && data.LockedUntil && (
              <div className="mt-1 text-rose-500">
                Locked until {dayjs(data.LockedUntil).format("HH:mm")} after
                5 failed attempts.
              </div>
            )}
          </div>
          <div className="flex flex-wrap gap-2">
            <OzButton
              variant="ghost"
              type="button"
              onClick={handleRegenerate}
              data-cy="mfa-regenerate-backup-codes"
            >
              Regenerate backup codes
            </OzButton>
            <OzButton
              variant="ghost"
              type="button"
              onClick={handleDisable}
              data-cy="mfa-disable"
            >
              <ShieldOff size={14} /> Disable 2FA
            </OzButton>
          </div>

          {newCodes && (
            <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-4">
              <div className="text-[13px] font-semibold text-amber-700 dark:text-amber-300">
                New backup codes (save them now)
              </div>
              <div className="mt-2 grid grid-cols-2 gap-2 font-mono text-[13px]">
                {newCodes.map((c) => (
                  <div key={c}>{c}</div>
                ))}
              </div>
              <button
                type="button"
                className="mt-2 text-[12px] underline"
                onClick={() => setNewCodes(null)}
              >
                I&apos;ve saved them, hide
              </button>
            </div>
          )}
        </div>
      )}

      <MFAEnrollmentModal
        open={enrollOpen}
        onClose={() => setEnrollOpen(false)}
        onEnrolled={() => {
          void mutate();
          notify({
            title: "Two-factor authentication",
            description: "2FA enabled successfully.",
          });
        }}
      />
    </OzCard>
  );
}
