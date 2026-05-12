"use client";

import { ToggleSwitch } from "@components/ToggleSwitch";
import classNames from "classnames";
import React from "react";

// OzSettingsToggle — labeled toggle row for /settings/*. Label + desc
// on the left, ToggleSwitch on the right. The whole row is clickable
// (mirrors the legacy FancyToggleSwitch UX) so the operator can hit a
// large target without aiming at the small thumb. Visual mirror of
// the handoff's S5Toggle (screens-5.jsx).
//
// Wraps the existing Radix-backed ToggleSwitch component to preserve
// keyboard nav, accessibility, and the data-cy hook in legacy
// behavior; only the row's surrounding paint changes.

interface Props {
  value: boolean;
  onChange: (next: boolean) => void;
  label: React.ReactNode;
  /** Sub-line under the label. Renders 12px text-muted. */
  desc?: React.ReactNode;
  disabled?: boolean;
  dataCy?: string;
  /**
   * `nested` removes the row's own padding/divider so a parent can
   * compose it inside a card section without doubling spacing. The
   * caller is responsible for separating sibling rows in that case.
   */
  nested?: boolean;
}

export default function OzSettingsToggle({
  value,
  onChange,
  label,
  desc,
  disabled = false,
  dataCy,
  nested,
}: Readonly<Props>) {
  const onClick = () => {
    if (disabled) return;
    onChange(!value);
  };
  const onKeyDown = (event: React.KeyboardEvent) => {
    if (disabled) return;
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onClick();
    }
  };

  return (
    <div
      role="button"
      tabIndex={disabled ? -1 : 0}
      aria-pressed={value}
      aria-disabled={disabled}
      onClick={onClick}
      onKeyDown={onKeyDown}
      className={classNames(
        "flex cursor-pointer select-none items-center gap-4",
        nested ? "" : "px-1",
        disabled && "cursor-not-allowed opacity-60",
      )}
    >
      <div className="min-w-0 flex-1">
        <div className="text-[13.5px] font-medium leading-[1.35] text-oz2-text">
          {label}
        </div>
        {desc && (
          <div className="mt-[2px] text-[12px] leading-[1.5] text-oz2-text-muted">
            {desc}
          </div>
        )}
      </div>
      <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
        <ToggleSwitch
          checked={value}
          onCheckedChange={onChange}
          disabled={disabled}
          size="small"
          dataCy={dataCy}
          className="data-[state=checked]:!bg-oz2-acc data-[state=unchecked]:!bg-oz2-border-strong"
        />
      </div>
    </div>
  );
}
