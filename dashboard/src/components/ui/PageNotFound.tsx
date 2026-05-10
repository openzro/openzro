"use client";

import { CircleAlertIcon, Undo2Icon } from "lucide-react";
import { useRouter } from "next/navigation";
import * as React from "react";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";

// PageNotFound — v2-painted 404. Renders inside whatever page surface
// the consumer mounts it in (V2DashboardLayout provides the chrome).
// Centered OzCard with an alert glyph, title, body, and a "Go Back"
// OzButton. The legacy version overlaid a skeleton-on-blur shell
// inside a PageContainer; on v2 chrome that's heavy and visually
// redundant — the v2 layout already gives the page its own surface.

type Props = {
  title?: string;
  description?: string;
};

export const PageNotFound = ({
  title = "The requested page was not found",
  description = "The page you are attempting to access cannot be found. Please verify the URL or return to the dashboard to continue browsing.",
}: Props) => {
  const router = useRouter();

  return (
    <div className="flex min-h-[60vh] items-center justify-center p-8">
      <OzCard className="max-w-xl text-center">
        <div className="flex flex-col items-center gap-4 px-6 py-8">
          <div
            aria-hidden
            className="grid h-12 w-12 place-items-center rounded-full bg-oz2-acc-soft text-oz2-acc-text"
          >
            <CircleAlertIcon size={22} />
          </div>
          <div>
            <h1 className="text-[20px] font-semibold tracking-tight text-oz2-text first-letter:uppercase">
              {title}
            </h1>
            <p className="mx-auto mt-2 max-w-md text-[13.5px] leading-[1.6] text-oz2-text-muted">
              {description}
            </p>
          </div>
          <OzButton
            variant="default"
            type="button"
            onClick={() => router.back()}
          >
            <Undo2Icon size={13} />
            Go Back
          </OzButton>
        </div>
      </OzCard>
    </div>
  );
};
