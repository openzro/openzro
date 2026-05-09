"use client";

import classNames from "classnames";
import React from "react";

// v2 sidebar — 240px wide, sectioned nav, item rendering with
// icon + label + optional badge. Items + sections are passed in
// as data; the design language is owned here.
//
// Reference: design_handoff_openzro_dashboard/design/shell.jsx.
//
// This component does NOT know about routing — consumers wire
// `active` per-item and pass `onClick` (or render `<Link>` directly
// in the icon slot) to integrate with whatever router is in use.

export interface OzSidebarItem {
  id: string;
  label: string;
  icon: React.ReactNode;
  active?: boolean;
  badge?: React.ReactNode;
  onClick?: () => void;
}

export interface OzSidebarSection {
  id: string;
  label: string;
  items: OzSidebarItem[];
}

export interface OzSidebarProps {
  /** Brand mark + wordmark area at the top. */
  brand?: React.ReactNode;
  /** Search input (or any kbd-hint slot) below the brand. */
  search?: React.ReactNode;
  /** Sectioned nav items. */
  sections: OzSidebarSection[];
  /** Avatar / footer block. */
  footer?: React.ReactNode;
}

const OzSidebar = ({ brand, search, sections, footer }: OzSidebarProps) => {
  return (
    <nav className="flex h-full flex-col">
      {brand && (
        <div className="px-4 pt-4 pb-3">{brand}</div>
      )}
      {search && (
        <div className="px-3 pb-3">{search}</div>
      )}
      <div className="flex-1 overflow-y-auto px-3">
        {sections.map((section) => (
          <div key={section.id} className="mb-5">
            <p className="mb-2 px-3 font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
              {section.label}
            </p>
            <ul className="space-y-0.5">
              {section.items.map((item) => (
                <li key={item.id}>
                  <button
                    type="button"
                    onClick={item.onClick}
                    className={classNames(
                      "flex h-8 w-full items-center gap-2.5 rounded-lg px-3 text-[13px] font-medium transition-colors",
                      item.active
                        ? "bg-oz2-surface text-oz2-text shadow-oz2-sm border border-oz2-border-soft"
                        : "text-oz2-text-2 hover:bg-oz2-hover",
                    )}
                  >
                    <span
                      aria-hidden="true"
                      className="inline-flex h-4 w-4 shrink-0 items-center justify-center"
                    >
                      {item.icon}
                    </span>
                    <span className="truncate">{item.label}</span>
                    {item.badge && (
                      <span className="ml-auto">{item.badge}</span>
                    )}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
      {footer && (
        <div className="border-t border-oz2-border-soft p-3">{footer}</div>
      )}
    </nav>
  );
};

export default OzSidebar;
