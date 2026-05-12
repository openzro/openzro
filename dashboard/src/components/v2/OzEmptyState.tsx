"use client";

import { ChevronRight, Laptop, Plus, Server, Smartphone } from "lucide-react";
import * as React from "react";
import OpenzroIcon from "@/assets/icons/OpenzroIcon";
import OzCard from "@/components/v2/OzCard";

// OzEmptyState — shared cold-start surface for v2 list pages (Peers,
// Networks, etc). Renders the canonical mesh visual (concentric dashed
// rings + violet glow + Openzro core + 4 orbiting ghost peers) over a
// dotted-grid card, then the page's own title/description/CTA, with an
// optional row of helper cards underneath.
//
// Design source: design_handoff_openzro_dashboard/peers-empty.bundle.html.
// Copy is supplied by the consumer so existing legacy strings carry
// over — this component owns paint only.

export interface OzEmptyStateHelperCard {
  /** Lucide icon (or any ReactNode) shown in the violet square. */
  icon: React.ReactNode;
  title: string;
  description: string;
  href?: string;
  /** External links open in a new tab; defaults to true when href is http(s). */
  external?: boolean;
}

export interface OzEmptyStateProps {
  title: string;
  description: React.ReactNode;
  /** Primary CTA — typically an OzButton. */
  primaryAction?: React.ReactNode;
  /** Optional secondary CTA, rendered next to the primary. */
  secondaryAction?: React.ReactNode;
  /**
   * Trailing one-line helper text under the CTAs (e.g. "Learn more in
   * our Getting Started Guide"). Accepts ReactNode so consumers can
   * inline a link.
   */
  learnMore?: React.ReactNode;
  /**
   * Optional 3-up grid of secondary entry-points rendered below the
   * hero card. Renders nothing when the list is empty/undefined.
   */
  helperCards?: OzEmptyStateHelperCard[];
}

// 4 ghost-peer slots arranged on a circle around the core. The dashed
// "+" placeholder hints at the call-to-action without competing with
// the primary CTA below.
const ORBIT_NODES: { angle: number; icon: React.ReactNode; dashed?: boolean }[] = [
  { angle: -30, icon: <Laptop size={15} /> },
  { angle: 60, icon: <Server size={15} /> },
  { angle: 150, icon: <Smartphone size={15} /> },
  { angle: 240, icon: <Plus size={15} />, dashed: true },
];

const ORBIT_RADIUS = 110;

export default function OzEmptyState({
  title,
  description,
  primaryAction,
  secondaryAction,
  learnMore,
  helperCards,
}: OzEmptyStateProps) {
  return (
    <div>
      <OzCard
        flush
        className="relative overflow-hidden px-7 pb-11 pt-14"
      >
        <DottedGridBackdrop />

        <MeshVisual />

        <div className="relative mx-auto max-w-[520px] text-center">
          <h2 className="m-0 text-[24px] font-semibold tracking-[-0.022em] text-oz2-text">
            {title}
          </h2>
          <p className="mx-auto mt-2.5 max-w-[440px] text-[14px] leading-[1.55] text-oz2-text-muted">
            {description}
          </p>

          {(primaryAction || secondaryAction) && (
            <div className="mt-[22px] inline-flex flex-wrap items-center justify-center gap-2.5">
              {primaryAction}
              {secondaryAction}
            </div>
          )}

          {learnMore && (
            <p className="mt-5 text-[12.5px] text-oz2-text-faint">
              {learnMore}
            </p>
          )}
        </div>
      </OzCard>

      {helperCards && helperCards.length > 0 && (
        <div className="mt-[18px] grid gap-3 md:grid-cols-3">
          {helperCards.map((card) => (
            <HelperCard key={card.title} {...card} />
          ))}
        </div>
      )}
    </div>
  );
}

// Dotted radial grid behind the mesh. Pure decoration — sits absolutely
// inside the card and fades out toward the edges via a radial mask so
// it never reaches the card border.
function DottedGridBackdrop() {
  return (
    <div
      aria-hidden
      className="pointer-events-none absolute inset-0 opacity-70"
      style={{
        backgroundImage:
          "radial-gradient(circle at 1px 1px, color-mix(in oklab, var(--ozv2-text) 7%, transparent) 1px, transparent 0)",
        backgroundSize: "22px 22px",
        WebkitMaskImage:
          "radial-gradient(60% 70% at 50% 40%, #000 30%, transparent 75%)",
        maskImage:
          "radial-gradient(60% 70% at 50% 40%, #000 30%, transparent 75%)",
      }}
    />
  );
}

// Centered mesh emblem: outer + inner dashed rings spinning slowly in
// opposite directions, soft violet glow, square Openzro core, and four
// orbiting ghost peers placed by trig. The whole block is purely
// decorative (`aria-hidden` would be redundant since it has no labels).
function MeshVisual() {
  return (
    <div className="relative mx-auto mb-7 grid h-60 w-60 place-items-center">
      <span
        aria-hidden
        className="absolute inset-0 rounded-full"
        style={{
          border:
            "1px dashed color-mix(in oklab, var(--ozv2-acc) 35%, transparent)",
          animation: "ozspin 60s linear infinite",
        }}
      />
      <span
        aria-hidden
        className="absolute inset-9 rounded-full"
        style={{
          border:
            "1px dashed color-mix(in oklab, var(--ozv2-acc) 22%, transparent)",
          animation: "ozspin 90s linear infinite reverse",
        }}
      />
      <span
        aria-hidden
        className="absolute h-40 w-40 rounded-full"
        style={{
          background:
            "radial-gradient(circle, color-mix(in oklab, var(--ozv2-acc) 30%, transparent) 0%, transparent 65%)",
          filter: "blur(8px)",
        }}
      />

      <span
        className="relative grid h-16 w-16 place-items-center rounded-[18px] overflow-hidden"
        style={{
          // The Openzro brand disc already carries the violet gradient,
          // so the core is just the icon at full bleed with a soft
          // violet drop shadow underneath. No inner color box.
          boxShadow:
            "0 14px 36px rgba(124,58,237,0.42), 0 1px 0 rgba(255,255,255,0.30) inset",
        }}
      >
        <OpenzroIcon size={64} />
      </span>

      {ORBIT_NODES.map((node) => {
        const x = Math.cos((node.angle * Math.PI) / 180) * ORBIT_RADIUS;
        const y = Math.sin((node.angle * Math.PI) / 180) * ORBIT_RADIUS;
        return (
          <span
            key={node.angle}
            aria-hidden
            className={
              "absolute left-1/2 top-1/2 grid h-9 w-9 place-items-center rounded-[10px] " +
              (node.dashed
                ? "border border-dashed text-oz2-acc"
                : "border border-oz2-border bg-oz2-surface text-oz2-text-muted shadow-oz2-sm")
            }
            style={{
              transform: `translate(calc(-50% + ${x}px), calc(-50% + ${y}px))`,
              borderColor: node.dashed
                ? "color-mix(in oklab, var(--ozv2-acc) 50%, transparent)"
                : undefined,
              borderWidth: node.dashed ? 1.5 : undefined,
            }}
          >
            {node.icon}
          </span>
        );
      })}
    </div>
  );
}

function HelperCard({
  icon,
  title,
  description,
  href,
  external,
}: OzEmptyStateHelperCard) {
  const isExternal =
    external ?? (typeof href === "string" && /^https?:\/\//.test(href));
  const externalProps = isExternal
    ? { target: "_blank" as const, rel: "noopener noreferrer" as const }
    : {};

  const inner = (
    <>
      <span className="grid h-8 w-8 flex-shrink-0 place-items-center rounded-[9px] bg-oz2-acc-soft text-oz2-acc-text">
        {icon}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="text-[13.5px] font-semibold text-oz2-text">
            {title}
          </span>
          <span className="ml-auto text-oz2-text-faint">
            <ChevronRight size={12} />
          </span>
        </div>
        <div className="mt-[3px] text-[12.5px] leading-[1.45] text-oz2-text-muted">
          {description}
        </div>
      </div>
    </>
  );

  const className =
    "flex items-start gap-3 rounded-oz2-card border border-oz2-border bg-oz2-surface p-3.5 shadow-oz2-sm transition-colors hover:border-oz2-border-strong hover:bg-oz2-hover";

  if (href) {
    return (
      <a href={href} className={className} {...externalProps}>
        {inner}
      </a>
    );
  }
  return <div className={className}>{inner}</div>;
}
