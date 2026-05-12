import { IconArrowBack } from "@tabler/icons-react";
import { cn } from "@utils/helpers";
import { SearchIcon } from "lucide-react";
import * as React from "react";
import { forwardRef } from "react";

// DropdownInput — search-input head used at the top of cmdk-style
// popovers (currently only GlobalSearchModal). v2 paint: oz2 border /
// surface tokens, faint Search glyph on the left, optional <kbd>
// Enter hint on the right. The hard-zero rule in globals.css keeps
// Flowbite's base form-input border from ever showing through on
// focus — paired here with `outline-none` and an explicit
// `border-transparent` so the input is visually flat against the
// elevated popover surface.

type Props = {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  hideEnterIcon?: boolean;
  className?: string;
} & React.InputHTMLAttributes<HTMLInputElement>;

export const DropdownInput = forwardRef<HTMLInputElement, Props>(
  (
    {
      value,
      onChange,
      placeholder = "Search...",
      className,
      hideEnterIcon = false,
      ...props
    },
    ref,
  ) => {
    return (
      <div className="relative w-full border-b border-oz2-border-soft">
        <input
          ref={ref}
          className={cn(
            "h-11 w-full bg-transparent pl-10 pr-12 text-[13px] text-oz2-text",
            "placeholder:text-oz2-text-faint outline-none",
            "border-0",
            className,
          )}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          {...props}
        />
        <div className="pointer-events-none absolute left-0 top-0 flex h-full items-center pl-4 text-oz2-text-faint">
          <SearchIcon size={14} />
        </div>
        {!hideEnterIcon && (
          <div className="absolute right-0 top-0 flex h-full items-center pr-3">
            <kbd
              className="inline-flex items-center gap-1 rounded-[5px] border border-oz2-border-soft bg-oz2-bg-sunken px-1.5 py-[3px] font-mono text-[10.5px] text-oz2-text-faint"
              aria-label="Press Enter"
            >
              <IconArrowBack size={10} />
            </kbd>
          </div>
        )}
      </div>
    );
  },
);

DropdownInput.displayName = "DropdownInput";
