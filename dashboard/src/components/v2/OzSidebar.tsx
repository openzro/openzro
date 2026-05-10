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
  /**
   * Icon-only mode (shadcn pattern). Hides section labels, item
   * labels, and badges; centers icons. Brand + footer are still
   * rendered but expected to render their own collapsed variants.
   */
  collapsed?: boolean;
}

const OzSidebar = ({
  brand,
  search,
  sections,
  footer,
  collapsed = false,
}: OzSidebarProps) => {
  return (
    <nav className="flex h-full flex-col">
      {brand && (
        <div
          className={classNames(
            "pt-4 pb-3",
            collapsed ? "grid place-items-center px-2" : "px-4",
          )}
        >
          {brand}
        </div>
      )}
      {search && !collapsed && <div className="px-3 pb-3">{search}</div>}
      <div
        className={classNames(
          "flex-1 overflow-y-auto",
          collapsed ? "px-2" : "px-3",
        )}
      >
        {sections.map((section) => (
          <div key={section.id} className="mb-5">
            {!collapsed && (
              <p className="mb-2 px-3 font-mono text-[12.5px] uppercase tracking-widest text-oz2-text-faint">
                {section.label}
              </p>
            )}
            <ul className="space-y-0.5">
              {section.items.map((item) => (
                <li key={item.id}>
                  <button
                    type="button"
                    onClick={item.onClick}
                    title={collapsed ? item.label : undefined}
                    aria-label={collapsed ? item.label : undefined}
                    className={classNames(
                      "flex h-8 items-center rounded-lg text-[15px] font-medium transition-colors",
                      collapsed
                        ? "w-full justify-center"
                        : "w-full gap-2.5 px-3",
                      item.active
                        ? "border border-oz2-border-soft bg-oz2-surface text-oz2-text shadow-oz2-sm"
                        : "text-oz2-text-2 hover:bg-oz2-hover",
                    )}
                  >
                    <span
                      aria-hidden="true"
                      className="inline-flex h-4 w-4 shrink-0 items-center justify-center"
                    >
                      {item.icon}
                    </span>
                    {!collapsed && (
                      <>
                        <span className="truncate">{item.label}</span>
                        {item.badge && (
                          <span className="ml-auto">{item.badge}</span>
                        )}
                      </>
                    )}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
      {footer && (
        <div
          className={classNames(
            "border-t border-oz2-border-soft",
            collapsed ? "p-2" : "p-3",
          )}
        >
          {footer}
        </div>
      )}
    </nav>
  );
};

export default OzSidebar;
