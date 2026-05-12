"use client";

import DescriptionWithTooltip from "@components/ui/DescriptionWithTooltip";
import TextWithTooltip from "@components/ui/TextWithTooltip";
import * as React from "react";
import OzStatusDot from "@/components/v2/OzStatusDot";
import { PostureCheck } from "@/interfaces/PostureCheck";

// V2 paint of PostureCheckNameCell — OzStatusDot + name + optional
// description tooltip. Replaces the legacy ActiveInactiveRow / nb-gray
// chain with v2 tokens.

type Props = {
  check: PostureCheck & { active?: boolean };
};

export const PostureCheckNameCellV2 = ({ check }: Props) => {
  return (
    <div className="flex min-w-0 items-start gap-2.5">
      <OzStatusDot
        status={check.active ? "on" : "off"}
        className="mt-[5px] shrink-0"
      />
      <div className="flex min-w-0 flex-col">
        <div className="flex items-center gap-2 font-medium text-oz2-text">
          <TextWithTooltip text={check.name} maxChars={30} />
        </div>
        <DescriptionWithTooltip
          className="mt-0.5 text-[12.5px] text-oz2-text-muted"
          text={check.description}
          maxChars={50}
        />
      </div>
    </div>
  );
};
