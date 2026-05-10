"use client";

import classNames from "classnames";
import React, { forwardRef } from "react";

// v2 table primitives — shadcn pattern (forwardRef + cn + displayName)
// painted with --ozv2-* tokens. Mirror of
// dashboard/src/components/table/Table.tsx (legacy nb-gray paint) so
// phase-4 screens compose with the same shape as the production
// DataTable but get the v2 visual.

const OzTable = forwardRef<
  HTMLTableElement,
  React.HTMLAttributes<HTMLTableElement>
>(({ className, ...props }, ref) => (
  <div className="relative w-full overflow-x-auto">
    <table
      ref={ref}
      className={classNames("w-full caption-bottom text-[15px]", className)}
      {...props}
    />
  </div>
));
OzTable.displayName = "OzTable";

const OzTableHeader = forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <thead
    ref={ref}
    className={classNames("bg-oz2-bg-sunken", className)}
    {...props}
  />
));
OzTableHeader.displayName = "OzTableHeader";

const OzTableBody = forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <tbody
    ref={ref}
    className={classNames("[&_tr:last-child]:border-0", className)}
    {...props}
  />
));
OzTableBody.displayName = "OzTableBody";

const OzTableRow = forwardRef<
  HTMLTableRowElement,
  React.HTMLAttributes<HTMLTableRowElement>
>(({ className, ...props }, ref) => (
  <tr
    ref={ref}
    className={classNames(
      "group border-b border-oz2-border-soft transition-colors",
      "hover:bg-oz2-hover data-[state=selected]:bg-oz2-acc-soft/40",
      className,
    )}
    {...props}
  />
));
OzTableRow.displayName = "OzTableRow";

const OzTableHead = forwardRef<
  HTMLTableCellElement,
  React.ThHTMLAttributes<HTMLTableCellElement>
>(({ className, ...props }, ref) => (
  <th
    ref={ref}
    className={classNames(
      "whitespace-nowrap px-[14px] py-[11px] text-left font-mono text-[12px] font-semibold uppercase tracking-widest text-oz2-text-muted",
      className,
    )}
    {...props}
  />
));
OzTableHead.displayName = "OzTableHead";

const OzTableCell = forwardRef<
  HTMLTableCellElement,
  React.TdHTMLAttributes<HTMLTableCellElement>
>(({ className, ...props }, ref) => (
  <td
    ref={ref}
    className={classNames("px-[14px] py-[13px] align-middle", className)}
    {...props}
  />
));
OzTableCell.displayName = "OzTableCell";

export {
  OzTable,
  OzTableHeader,
  OzTableBody,
  OzTableRow,
  OzTableHead,
  OzTableCell,
};
