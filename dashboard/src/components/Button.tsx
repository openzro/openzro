"use client";

import { cva, VariantProps } from "class-variance-authority";
import classNames from "classnames";
import React, { forwardRef } from "react";

export type ButtonVariants = VariantProps<typeof buttonVariants>;

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    ButtonVariants {
  disabled?: boolean;
  stopPropagation?: boolean;
}

export const buttonVariants = cva(
  [
    "relative",
    "text-sm focus:z-10 focus:ring-2 font-medium  focus:outline-none whitespace-nowrap shadow-sm",
    "inline-flex gap-2 items-center justify-center transition-colors focus:ring-offset-1",
    "disabled:opacity-20 disabled:cursor-not-allowed disabled:dark:text-nb-gray-300 dark:ring-offset-neutral-950/50",
  ],
  {
    variants: {
      variant: {
        default: [
          "bg-white hover:text-black focus:ring-zinc-200/50  hover:bg-neutral-200 border-neutral-300 text-gray-900",
          "dark:focus:ring-zinc-800/50 dark:bg-nb-gray dark:text-gray-400 dark:border-gray-700/30 dark:hover:text-white dark:hover:bg-zinc-800/50",
        ],
        primary: [
          "dark:focus:ring-openzro-600/50 dark:ring-offset-neutral-950/50 enabled:dark:bg-openzro disabled:dark:bg-nb-gray-910 dark:text-gray-100 enabled:dark:hover:text-white enabled:dark:hover:bg-openzro-500/80",
          "enabled:bg-openzro enabled:text-white enabled:focus:ring-openzro-400/50 enabled:hover:bg-openzro-500",
        ],
        secondary: [
          "bg-white hover:text-black focus:ring-zinc-200/50 hover:bg-neutral-200 border-neutral-300 text-gray-900",
          "dark:ring-offset-neutral-950/50 dark:focus:ring-neutral-500/20  ",
          "dark:bg-nb-gray-900/30 dark:text-gray-400 dark:border-gray-700/40 dark:hover:text-white dark:hover:bg-zinc-800/50",
        ],
        secondaryLighter: [
          "bg-white hover:text-black focus:ring-zinc-200/50 hover:bg-neutral-200 border-neutral-300 text-gray-900",
          "dark:ring-offset-neutral-950/50 dark:focus:ring-neutral-500/20  ",
          "dark:bg-nb-gray-900/70 dark:text-gray-400 dark:border-gray-700/70 dark:hover:text-white dark:hover:bg-nb-gray-800/60",
        ],
        input: [
          "bg-white hover:text-black focus:ring-zinc-200/50 hover:bg-neutral-200 border-neutral-300 text-gray-900",
          "dark:ring-offset-neutral-950/50 dark:focus:ring-neutral-500/20  ",
          "dark:bg-nb-gray-900  dark:text-gray-400  dark:border-nb-gray-700 dark:hover:bg-nb-gray-900/80",
        ],
        dropdown: [
          "bg-white hover:text-black focus:ring-zinc-200/50 hover:bg-neutral-200 border-neutral-300 text-gray-900",
          "dark:ring-offset-neutral-950/50 dark:focus:ring-neutral-500/20  ",
          "dark:bg-nb-gray-900/40 dark:text-gray-400 dark:border-nb-gray-900 dark:hover:bg-nb-gray-900/50",
        ],
        dotted: [
          "bg-white hover:text-black focus:ring-zinc-200/50 hover:bg-neutral-200 border-neutral-300 text-gray-900 border-dashed",
          "dark:ring-offset-neutral-950/50 dark:focus:ring-neutral-500/20  ",
          "dark:bg-nb-gray-900/30 dark:text-gray-400 dark:border-gray-500/40 dark:hover:text-white dark:hover:bg-zinc-800/50",
        ],
        tertiary: [
          "bg-white hover:text-black focus:ring-zinc-200/50  hover:bg-neutral-200 border-neutral-300 text-gray-900",
          "dark:focus:ring-zinc-800/50 dark:bg-white dark:text-gray-800 dark:border-gray-700/40 dark:hover:bg-neutral-200 disabled:dark:bg-nb-gray-920 disabled:dark:text-nb-gray-300",
        ],
        white: [
          "focus:ring-white/50 bg-white text-gray-800 border-white outline-none hover:bg-neutral-200 disabled:dark:bg-nb-gray-920 disabled:dark:text-nb-gray-300",
          "disabled:dark:bg-nb-gray-900 disabled:dark:text-nb-gray-300 disabled:dark:border-nb-gray-900",
        ],
        outline: [
          // Brand-outlined button: transparent surface, openZro
          // accent text + border in both themes so the brand
          // identity reads. Hover lifts to the soft brand-50 chip.
          "bg-transparent text-openzro-700 border-openzro-300 hover:bg-openzro-50 focus:ring-openzro-300/50",
          "dark:focus:ring-zinc-800/50 dark:bg-transparent dark:text-openzro dark:border-openzro dark:hover:bg-nb-gray-900/30",
        ],
        "danger-outline": [
          // Light: red text + red-300 border, soft red-50 hover.
          // Same visual weight as `outline` but with a destructive
          // signal — used for irreversible actions where solid red
          // is too aggressive for the surrounding context.
          "bg-transparent text-red-700 border-red-300 enabled:hover:bg-red-50 enabled:hover:border-red-400 focus:ring-red-400/30",
          "enabled:dark:focus:ring-red-800/20 enabled:dark:focus:bg-red-950/40 enabled:hover:dark:bg-red-950/50 enabled:dark:hover:border-red-800/50 dark:bg-transparent dark:text-red-500",
        ],
        "default-outline": [
          // Ghost button: transparent in both themes, picks up a
          // soft surface on hover so it reads as interactive without
          // competing with adjacent solid buttons. Border stays
          // transparent until hover.
          "bg-transparent text-neutral-600 border-transparent hover:text-neutral-900 hover:bg-neutral-100 hover:border-neutral-200 focus:ring-neutral-300/40",
          "dark:ring-offset-nb-gray-950/50 dark:focus:ring-nb-gray-500/20",
          "dark:bg-transparent dark:text-nb-gray-400 dark:border-transparent dark:hover:text-white dark:hover:bg-nb-gray-900/30 dark:hover:border-nb-gray-800/50",
        ],
        danger: [
          // Light: solid red surface with white text — same visual
          // weight as the primary openZro CTA so the destructive
          // intent reads at a glance against any background. The
          // ring matches the surface so focus is visible without
          // changing the colour family.
          "bg-red-600 text-white border-red-600 hover:bg-red-700 hover:border-red-700 focus:ring-red-400/50",
          "dark:focus:ring-red-700/20 dark:focus:bg-red-700 hover:dark:bg-red-700 dark:hover:border-red-800/50 dark:bg-red-600 dark:text-red-100",
        ],
      },
      size: {
        xs: "text-xs py-2 px-4",
        xs2: "text-[0.78rem] py-2 px-4",
        sm: "text-sm py-2.5 px-4",
        md: "text-md py-2.5 px-4",
        lg: "text-lg py-2.5 px-4",
      },
      rounded: {
        true: "rounded-md",
        false: "",
      },
      border: {
        0: "border",
        // The previous "border border-transparent" baked an explicit
        // transparent colour into every button, which Tailwind then
        // resolved against the variant's `border-X` because this
        // `border` slot is declared LAST in the variants object — so
        // the variant's colour was always overridden in light mode.
        // Dark theme escaped because `dark:border-X` has higher CSS
        // specificity than the bare `border-transparent`. Drop the
        // colour reset; let the variants own the border colour.
        1: "border",
        2: "border border-t-0 border-b-0",
      },
    },
  },
);

const Button = forwardRef(
  (
    {
      variant = "default",
      rounded = true,
      border = 1,
      size = "md",
      stopPropagation = true,
      ...props
    }: ButtonProps,
    ref: React.ForwardedRef<HTMLButtonElement>,
  ) => {
    return (
      <button
        type="button"
        {...props}
        ref={ref}
        className={classNames(
          buttonVariants({
            variant,
            rounded,
            border: border ? 1 : 0,
            size: size,
          }),
          props.className,
        )}
        onClick={(e) => {
          stopPropagation && e.stopPropagation();
          props.onClick && props.onClick(e);
        }}
      >
        {props.children}
      </button>
    );
  },
);

Button.displayName = "Button";

export default Button;
