import { cn } from "@utils/helpers";
import { cva, type VariantProps } from "class-variance-authority";
import React from "react";

// SquareIcon — square tile used as a glyph wrapper in modal headers
// and elsewhere. v2 paint: soft tinted background per semantic color
// (no dark *-950 fills, no shadow), 10px radius, no border. The
// caller's icon gets the matching foreground color automatically.

const iconVariant = cva(
  "flex items-center justify-center shrink-0 rounded-[10px] select-none",
  {
    variants: {
      color: {
        openzro: "bg-oz2-acc-soft text-oz2-acc-text",
        blue: "bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300",
        "blue-darker":
          "bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300",
        red: "bg-oz2-err-bg text-oz2-err",
        gray: "bg-oz2-bg-sunken text-oz2-text-2",
        green:
          "bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300",
        purple:
          "bg-violet-100 text-violet-700 dark:bg-violet-500/15 dark:text-violet-300",
        indigo:
          "bg-indigo-100 text-indigo-700 dark:bg-indigo-500/15 dark:text-indigo-300",
        yellow:
          "bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300",
      },
      size: {
        small: "w-8 h-8",
        medium: "w-10 h-10",
        large: "w-12 h-12",
      },
    },
  },
);

export type IconVariant = VariantProps<typeof iconVariant>;
interface Props extends IconVariant {
  icon: React.ReactNode;
  margin?: string;
  rounded?: boolean;
}

export default function SquareIcon({
  color = "openzro",
  icon,
  size = "medium",
  margin = "mt-1",
  rounded = false,
}: Props) {
  return (
    <div
      className={cn(
        iconVariant({
          color,
          size,
        }),
        margin,
        rounded && "!rounded-full",
      )}
    >
      {icon}
    </div>
  );
}
