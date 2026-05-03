import { cn } from "@utils/helpers";
import { cva, VariantProps } from "class-variance-authority";
import React from "react";

type BadgeVariants = VariantProps<typeof variants>;

interface Props extends React.HTMLAttributes<HTMLDivElement>, BadgeVariants {
  children: React.ReactNode;
}

const variants = cva("", {
  variants: {
    variant: {
      default: [
        "bg-neutral-200 border-neutral-300 text-neutral-700",
        "dark:bg-nb-gray-800 dark:border-nb-gray-700 dark:text-nb-gray-300",
      ],
      darker: [
        "bg-neutral-100 border-neutral-200 text-neutral-700",
        "dark:bg-nb-gray-930 dark:border-nb-gray-900 dark:text-nb-gray-250",
      ],
      openzro: ["bg-openzro-100 text-openzro border-openzro "],
    },
    size: {
      default: ["py-2.5 px-1.5 text-xs h-[12px]"],
      small: ["py-[9px] px-2 text-[9px] h-[12px] leading-[0]"],
    },
    disabled: {
      true: [
        "bg-neutral-200 border-neutral-300 text-neutral-700",
        "dark:bg-nb-gray-800 dark:border-nb-gray-700 dark:text-nb-gray-300",
      ],
    },
  },
});

export default function Kbd({
  children,
  variant = "default",
  size = "default",
  disabled = false,
  className,
}: Readonly<Props>) {
  return (
    <div
      className={cn(
        " shadow-sm border rounded-[4px]  leading-[0] flex gap-1 items-center",
        variants({ variant, size, disabled }),
        className,
      )}
    >
      {children}
    </div>
  );
}
