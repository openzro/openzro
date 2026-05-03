import { cn } from "@utils/helpers";
import { cva, VariantProps } from "class-variance-authority";
import { InfoIcon } from "lucide-react";
import * as React from "react";

type CalloutVariants = VariantProps<typeof calloutVariants>;

type Props = {
  icon?: React.ReactNode;
  children?: React.ReactNode;
  className?: string;
} & CalloutVariants;

export const calloutVariants = cva(
  ["px-4 py-3.5 rounded-md border text-sm font-normal flex gap-3 font-light"],
  {
    variants: {
      variant: {
        default: [
          "bg-neutral-100 border-neutral-200 text-neutral-700",
          "dark:bg-nb-gray-900/60 dark:border-nb-gray-800/80 dark:text-nb-gray-300",
        ].join(" "),
        warning: [
          "bg-openzro-50 border-openzro-200 text-openzro-800",
          "dark:bg-openzro-500/10 dark:border-openzro-400/20 dark:text-openzro-150",
        ].join(" "),
        info: [
          "bg-sky-50 border-sky-200 text-sky-800",
          "dark:bg-sky-400/10 dark:border-sky-400/20 dark:text-sky-100",
        ].join(" "),
      },
    },
  },
);

export const Callout = ({
  children,
  icon = <InfoIcon size={14} className={"shrink-0 relative top-[2px]"} />,
  className,
  variant = "default",
}: Props) => {
  return (
    <div className={cn(calloutVariants({ variant }), className)}>
      {icon}
      <div>{children}</div>
    </div>
  );
};
