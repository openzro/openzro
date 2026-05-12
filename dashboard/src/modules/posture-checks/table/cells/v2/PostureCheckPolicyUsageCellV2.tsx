"use client";

import FullTooltip from "@components/FullTooltip";
import { ArrowUpRightSquareIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import * as React from "react";
import AccessControlIcon from "@/assets/icons/AccessControlIcon";
import OzPill from "@/components/v2/OzPill";
import { Policy } from "@/interfaces/Policy";
import { PostureCheck } from "@/interfaces/PostureCheck";

// V2 paint of PostureCheckPolicyUsageCell — replaces the legacy
// Badge / Button mix with OzPill + an inline v2 button.

type Props = {
  check: PostureCheck & { policies?: Policy[] };
};

export const PostureCheckPolicyUsageCellV2 = ({ check }: Props) => {
  const router = useRouter();
  const hasPolicies = (check.policies?.length ?? 0) > 0;

  return (
    <div className="flex items-center gap-4">
      <FullTooltip
        disabled={!hasPolicies}
        content={
          <div className="max-w-lg text-xs">
            <span className="text-sm font-medium text-oz2-text">
              Assigned{" "}
              {check.policies && check.policies.length > 1
                ? "Policies"
                : "Policy"}
            </span>
            <div className="flex flex-wrap gap-2 pb-2 pt-3">
              {check.policies?.map((policy) => (
                <OzPill
                  key={policy.id}
                  variant="default"
                  className="justify-start"
                >
                  <AccessControlIcon size={12} />
                  {policy.name}
                </OzPill>
              ))}
            </div>
          </div>
        }
        interactive={false}
      >
        <OzPill
          onClick={(e) => {
            if (!hasPolicies) return;
            e.stopPropagation();
            router.push("/access-control");
          }}
          variant={hasPolicies ? "acc" : "default"}
          className={
            "min-w-[110px] justify-center font-medium " +
            (hasPolicies
              ? "cursor-pointer hover:bg-oz2-acc-soft/80"
              : "pointer-events-none opacity-30")
          }
        >
          <AccessControlIcon size={12} />
          <span>
            <span className="font-bold">
              {hasPolicies ? check.policies?.length : ""}
            </span>{" "}
            {!hasPolicies
              ? "No Policies"
              : check.policies && check.policies.length > 1
                ? "Policies"
                : "Policy"}
          </span>
        </OzPill>
      </FullTooltip>
      <FullTooltip
        content={
          <div className="max-w-[260px] text-xs">
            To assign this posture check to your policies, visit the Policies
            page.
          </div>
        }
        interactive={false}
      >
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            router.push("/access-control");
          }}
          className="inline-flex h-7 min-w-[130px] items-center justify-center gap-1.5 rounded-oz2-input border border-oz2-border bg-oz2-surface px-2.5 text-[12.5px] font-medium text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:border-oz2-border-strong"
        >
          <ArrowUpRightSquareIcon size={12} />
          Go to Policies
        </button>
      </FullTooltip>
    </div>
  );
};
