"use client";

import classNames from "classnames";
import React from "react";

// v2 dashboard shell — composes Sidebar (240px) + Topbar (56px) +
// scrolling content area. Reference: design_handoff_openzro_dashboard/
// design/shell.jsx.
//
// API is intentionally minimal — slots for sidebar/topbar/children.
// Concrete OzSidebar / OzTopbar live alongside; consumers can also
// pass their own component if they want a non-standard chrome
// (auth pages, error pages, full-bleed dashboards).

export interface OzShellProps extends React.HTMLAttributes<HTMLDivElement> {
  sidebar: React.ReactNode;
  topbar: React.ReactNode;
}

const OzShell = ({
  sidebar,
  topbar,
  children,
  className,
  ...props
}: OzShellProps) => {
  return (
    <div
      className={classNames(
        "flex h-screen w-full overflow-hidden bg-oz2-bg font-sans text-oz2-text",
        className,
      )}
      {...props}
    >
      <aside className="flex h-full w-60 shrink-0 flex-col border-r border-oz2-border-soft bg-oz2-bg-soft">
        {sidebar}
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="h-14 shrink-0 border-b border-oz2-border-soft bg-oz2-bg/95 backdrop-blur-md">
          {topbar}
        </header>
        <main className="flex-1 overflow-y-auto overflow-x-hidden">
          {children}
        </main>
      </div>
    </div>
  );
};

export default OzShell;
