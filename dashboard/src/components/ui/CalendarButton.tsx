import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import * as React from "react";
import { cn } from "@/utils/helpers";

// CalendarButton — repainted with oz2-* tokens so the nav chevrons
// (and any other Calendar-internal buttons that reach for these
// variants) sit on theme regardless of light/dark. The legacy
// neutral-* / nb-gray-* palette painted the popover with cold gray
// chrome that conflicted with the v2 violet surface.
const buttonVariants = cva(
  "inline-flex items-center justify-center whitespace-nowrap rounded-oz2-input text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-oz2-acc/40 focus-visible:ring-offset-2 focus-visible:ring-offset-oz2-surface disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        default:
          "bg-oz2-acc text-oz2-text-on-acc hover:bg-oz2-acc-hover",
        destructive:
          "bg-oz2-err text-oz2-text-on-acc hover:bg-oz2-err/90",
        outline:
          "border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:bg-oz2-hover hover:border-oz2-border-strong hover:text-oz2-text",
        secondary:
          "bg-oz2-bg-sunken text-oz2-text hover:bg-oz2-hover",
        ghost:
          "text-oz2-text-2 hover:bg-oz2-hover hover:text-oz2-text",
        link: "text-oz2-acc-text underline-offset-4 hover:underline",
      },
      size: {
        default: "h-10 px-4 py-2",
        sm: "h-9 rounded-md px-3",
        lg: "h-11 rounded-md px-8",
        icon: "h-10 w-10",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const CalendarButton = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return (
      <Comp
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        {...props}
      />
    );
  },
);
CalendarButton.displayName = "Button";

export { buttonVariants, CalendarButton };
