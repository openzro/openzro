import { Input } from "@components/Input";
import { useDebounce } from "@hooks/useDebounce";
import { Search } from "lucide-react";
import React, { useEffect, useState } from "react";

interface Props extends React.InputHTMLAttributes<HTMLInputElement> {
  setGlobalSearch: (value: string) => void;
  globalSearch?: string;
  className?: string;
  isLoading?: boolean;
  onClick?: () => void;
}

// DataTableGlobalSearch — legacy DataTable's free-text filter input.
// The earlier "⌘ K" kbd hint + react-hotkeys-hook binding was dropped
// because: (a) Firefox hijacks ⌘ K for its own quick-search bar
// regardless of preventDefault, (b) the v2 table search inputs are
// bare (no kbd badge), so the legacy hint stood out as broken-by-
// promise. If a real global-command palette ships later, it should
// own the binding + visual cue and address every consumer at once.

export default function DataTableGlobalSearch({
  setGlobalSearch,
  globalSearch,
  className = "min-w-[300px] max-w-[400px] grow",
  isLoading,
  onClick,
  ...props
}: Readonly<Props>) {
  const [inputValue, setInputValue] = useState(globalSearch || "");
  const debouncedValue = useDebounce(inputValue, 800);

  useEffect(() => {
    setGlobalSearch(debouncedValue);
  }, [debouncedValue]);

  useEffect(() => {
    if (globalSearch !== undefined && globalSearch !== inputValue) {
      setInputValue(globalSearch);
    }
  }, [globalSearch]);

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setInputValue(e.target.value);
  };

  return (
    <Input
      {...props}
      onFocus={(e) => {
        if (onClick) {
          e.preventDefault();
          e.stopPropagation();
          onClick?.();
        }
      }}
      icon={<Search size={15} />}
      value={inputValue}
      onChange={handleChange}
      maxWidthClass={className}
      disabled={false}
    />
  );
}
