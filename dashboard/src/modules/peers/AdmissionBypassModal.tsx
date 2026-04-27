"use client";

import Button from "@components/Button";
import HelpText from "@components/HelpText";
import { Input } from "@components/Input";
import { Label } from "@components/Label";
import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import Paragraph from "@components/Paragraph";
import { Textarea } from "@components/Textarea";
import { useApiCall } from "@utils/api";
import { ShieldHalf } from "lucide-react";
import React, { useState } from "react";
import { useSWRConfig } from "swr";
import { AdmissionBypass } from "@/interfaces/AdmissionBypass";

type Props = {
  open: boolean;
  setOpen: (open: boolean) => void;
  peerId: string;
  peerName: string;
};

const PRESETS: { label: string; seconds: number }[] = [
  { label: "1 hour", seconds: 60 * 60 },
  { label: "4 hours", seconds: 4 * 60 * 60 },
  { label: "24 hours", seconds: 24 * 60 * 60 },
  { label: "7 days", seconds: 7 * 24 * 60 * 60 },
  { label: "30 days", seconds: 30 * 24 * 60 * 60 },
];

// AdmissionBypassModal grants a time-bounded admission bypass on a
// peer that's failing the Device Admission gate. ADR-0004 requires
// an audited initiator + reason + expiry — the reason and expiry
// are the only operator-supplied fields; the initiator is the
// caller's user ID, set server-side from the JWT.
export default function AdmissionBypassModal({
  open,
  setOpen,
  peerId,
  peerName,
}: Readonly<Props>) {
  const [reason, setReason] = useState("");
  const [presetSeconds, setPresetSeconds] = useState<number>(
    PRESETS[2].seconds, // default 24h — the most common break-glass window
  );
  const [saving, setSaving] = useState(false);
  const api = useApiCall<AdmissionBypass>(
    `/peers/${peerId}/admission-bypass`,
  );
  const { mutate } = useSWRConfig();

  const onSave = async () => {
    if (!reason.trim()) {
      notify({
        title: "Cannot grant bypass",
        description: "A reason is required for the audit trail.",
        promise: Promise.reject(new Error("reason required")),
        loadingMessage: "Granting admission bypass...",
      });
      return;
    }
    setSaving(true);
    try {
      await api.post({
        reason: reason.trim(),
        expires_in_seconds: presetSeconds,
      });
      await mutate("/peers");
      notify({
        title: "Bypass granted",
        description: `${peerName} can now connect for ${
          PRESETS.find((p) => p.seconds === presetSeconds)?.label ??
          `${presetSeconds}s`
        }.`,
      });
      setReason("");
      setOpen(false);
    } catch {
      // useApiCall surfaces the toast
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal open={open} onOpenChange={setOpen} key={open ? "open" : "closed"}>
      <ModalContent maxWidthClass={"max-w-xl"} showClose={true}>
        <ModalHeader
          icon={<ShieldHalf size={19} />}
          title={`Grant admission bypass for ${peerName}`}
          description={
            "Time-bounded override of the Device Admission gate. The grant is recorded with your user ID, the reason, and the expiry — the auditor sees the full lifecycle (granted → revoked or expired)."
          }
          color={"openzro"}
        />

        <div className={"flex flex-col gap-4 px-8 pb-2"}>
          <div>
            <Label>Reason</Label>
            <Textarea
              rows={3}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={
                "e.g. Intune re-enrol pending — board meeting at 14:00"
              }
            />
            <HelpText>
              Required for the audit trail. Free text — make it
              specific enough that an auditor reviewing this row in
              six months can reconstruct the situation.
            </HelpText>
          </div>

          <div>
            <Label>Expiry</Label>
            <div className={"mt-2 grid grid-cols-5 gap-2"}>
              {PRESETS.map((p) => (
                <button
                  key={p.seconds}
                  type={"button"}
                  onClick={() => setPresetSeconds(p.seconds)}
                  className={
                    "rounded-md border px-2 py-1.5 text-xs transition " +
                    (presetSeconds === p.seconds
                      ? "border-violet-500 bg-violet-500/10 text-violet-200"
                      : "border-nb-gray-800 hover:border-nb-gray-700 text-nb-gray-200")
                  }
                >
                  {p.label}
                </button>
              ))}
            </div>
            <HelpText>
              No-expiry bypasses are not permitted. Maximum is 30
              days; longer windows must be re-granted.
            </HelpText>
          </div>

          <Paragraph className={"text-xs text-nb-gray-300"}>
            The bypass applies only to the admission gate. Per-policy
            posture checks still run, so the peer continues to
            respect the ACL rules of every policy whose source
            posture-check list it fails.
          </Paragraph>
        </div>

        <ModalFooter>
          <ModalClose asChild>
            <Button variant={"secondary"} disabled={saving}>
              Cancel
            </Button>
          </ModalClose>
          <Button
            variant={"primary"}
            onClick={onSave}
            disabled={saving || !reason.trim()}
          >
            {saving ? "Granting…" : "Grant bypass"}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
