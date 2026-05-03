import { cn } from "@utils/helpers";
import { cva, VariantProps } from "class-variance-authority";
import * as React from "react";

export type BadgeVariants = VariantProps<typeof variants>;

interface Props extends React.HTMLAttributes<HTMLDivElement>, BadgeVariants {
  children: React.ReactNode;
  className?: string;
  useHover?: boolean;
  disabled?: boolean;
}

const variants = cva("", {
  variants: {
    variant: {
      blue: [
        "bg-sky-100 border-sky-500 text-sky-800 border border-transparent",
      ],
      blueDark: [
        "bg-sky-100 border-sky-300 text-sky-800 border",
        "dark:bg-sky-900 dark:border-sky-500 dark:text-white",
      ],
      "blue-darker": [
        "bg-sky-100 border-sky-300 text-sky-800 border",
        "dark:bg-sky-900 dark:border-sky-500 dark:text-white",
      ],
      red: ["bg-red-950/40 border-red-500 border text-red-500"],
      purple: ["bg-purple-950/50 border-purple-500 border text-purple-500"],
      yellow: ["bg-yellow-950 border-yellow-500 border text-yellow-400"],
      gray: [
        "bg-neutral-100 border-neutral-200 text-neutral-700",
        "dark:bg-nb-gray-930/60 dark:border-nb-gray-800/40 dark:text-nb-gray-300",
        "border",
      ],
      grayer: [
        "bg-neutral-100 border-neutral-200 text-neutral-700",
        "dark:bg-nb-gray-900/40 dark:border-nb-gray-800/40 dark:text-nb-gray-300",
        "border",
      ],
      "gray-ghost": [
        "bg-neutral-100 border-neutral-200 text-neutral-700",
        "dark:bg-nb-gray-900 dark:border-nb-gray-800 dark:text-nb-gray-300",
        "border dark:border-nb-gray-800/50",
      ],
      green: [
        "bg-green-100 border-green-300 text-green-800 border",
        "dark:bg-green-950 dark:border-green-500 dark:text-green-400",
      ],
      openzro: [
        // Light: soft brand-coloured chip mirroring how Tailwind's
        // accent-100/700 pair builds light-mode badges. Dark theme
        // keeps its existing deep-ink + accent-500 palette.
        "bg-openzro-100 border-openzro-300 text-openzro-700 border",
        "dark:bg-openzro-950 dark:border-openzro-500 dark:text-openzro-500",
      ],
    },
    hover: {
      none: [],
      blue: ["hover:bg-sky-200"],
      purple: ["hover:bg-purple-950/40"],
      yellow: ["hover:bg-yellow-950/40"],
      blueDark: ["hover:bg-sky-200 dark:hover:bg-sky-800"],
      "blue-darker": ["hover:bg-sky-200 dark:hover:bg-sky-800"],
      red: ["hover:bg-red-950/40"],
      gray: ["hover:bg-neutral-200 dark:hover:bg-nb-gray-900"],
      grayer: ["hover:bg-neutral-200 dark:hover:bg-nb-gray-900"],
      "gray-ghost": ["hover:bg-neutral-200 dark:hover:bg-nb-gray-900"],
      green: ["hover:bg-green-200 dark:hover:bg-green-950/50"],
      openzro: ["hover:bg-openzro-950/50"],
    },
  },
});

export default function Badge({
  children,
  className,
  variant = "blue",
  useHover = false,
  disabled = false,
  ...props
}: Readonly<Props>) {
  return (
    <div
      className={cn(
        "relative z-10 cursor-inherit whitespace-nowrap rounded-md text-[12px] py-1.5 px-3 font-normal flex gap-1.5 items-center justify-center transition-all",
        variants({ variant, hover: useHover ? variant : "none" }),
        disabled && "cursor-not-allowed opacity-50 select-none",
        className,
      )}
      {...props}
    >
      {children}
    </div>
  );
}
