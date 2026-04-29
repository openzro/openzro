"use client";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@components/DropdownMenu";
import { cn } from "@utils/helpers";
import { Check, Monitor, Moon, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import * as React from "react";

const THEMES = [
  { value: "light", label: "Light", Icon: Sun },
  { value: "dark", label: "Dark", Icon: Moon },
  { value: "system", label: "System", Icon: Monitor },
] as const;

export default function OzThemeSwitcher() {
  // next-themes is dynamic — needs to mount client-side before reading
  // resolvedTheme/theme to avoid SSR/CSR markup mismatch on the toggle
  // icon. mounted={false} renders the same neutral icon on both passes.
  const [mounted, setMounted] = React.useState(false);
  React.useEffect(() => setMounted(true), []);

  const { theme, setTheme, resolvedTheme } = useTheme();

  // The icon shown on the trigger reflects the *resolved* theme so
  // "System" mode still indicates whether the user is currently in
  // light or dark — useful when system preference flips overnight.
  const TriggerIcon = !mounted
    ? Moon
    : resolvedTheme === "light"
      ? Sun
      : Moon;

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        aria-label="Toggle theme"
        className={cn(
          "h-10 w-10 flex items-center justify-center rounded-md",
          "text-nb-gray-300 hover:text-white",
          "hover:bg-nb-gray-900/40 transition-colors",
          "focus:outline-none focus-visible:ring-2 focus-visible:ring-netbird-200",
        )}
      >
        <TriggerIcon size={18} />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-40">
        {THEMES.map(({ value, label, Icon }) => {
          const active = mounted && theme === value;
          return (
            <DropdownMenuItem
              key={value}
              onClick={() => setTheme(value)}
              className="gap-2"
            >
              <Icon size={14} className="text-nb-gray-300" />
              <span className="flex-1">{label}</span>
              {active && (
                <Check size={14} className="text-netbird-200" />
              )}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
