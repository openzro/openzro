"use client";

import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import OzPill from "@/components/v2/OzPill";
import OzStatusDot from "@/components/v2/OzStatusDot";

// Internal preview page for the v2 dashboard redesign — operators
// don't navigate here; it's a dev surface to validate the new
// primitives + tokens against the design handoff.
//
// Reach via /v2-preview when running `npm run dev`. Not linked from
// anywhere on purpose — when the migration is far enough along
// that primitives are widely adopted, this page will be deleted.

export default function V2PreviewPage() {
  return (
    <div className="min-h-screen bg-oz2-bg p-8 font-sans text-oz2-text">
      <div className="mx-auto max-w-5xl space-y-8">
        <header className="space-y-2">
          <p className="font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
            internal · dashboard redesign
          </p>
          <h1 className="text-[22px] font-semibold tracking-tight">
            v2 Primitives Preview
          </h1>
          <p className="text-[13px] text-oz2-text-muted">
            All four primitives rendered against the warm-paper / dark-violet
            tokens. Toggle the page theme via the existing top-right control
            to verify both modes.
          </p>
        </header>

        {/* OzButton */}
        <OzCard>
          <h2 className="mb-4 font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
            OzButton
          </h2>
          <div className="flex flex-wrap items-center gap-3">
            <OzButton variant="default">Default</OzButton>
            <OzButton variant="primary">Primary</OzButton>
            <OzButton variant="ghost">Ghost</OzButton>
            <OzButton variant="default" disabled>
              Disabled
            </OzButton>
            <OzButton variant="primary" disabled>
              Disabled primary
            </OzButton>
          </div>
        </OzCard>

        {/* OzCard */}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <OzCard>
            <p className="mb-1 font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
              KPI key
            </p>
            <p className="text-[22px] font-semibold tracking-tight">128</p>
            <p className="text-[11.5px] text-oz2-text-muted">
              Peers online · last 5 min
            </p>
          </OzCard>
          <OzCard>
            <p className="mb-1 font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
              KPI key
            </p>
            <p className="text-[22px] font-semibold tracking-tight">94%</p>
            <p className="text-[11.5px] text-oz2-text-muted">
              Compliant peers
            </p>
          </OzCard>
        </div>

        {/* OzPill */}
        <OzCard>
          <h2 className="mb-4 font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
            OzPill
          </h2>
          <div className="flex flex-wrap items-center gap-2">
            <OzPill variant="default">default</OzPill>
            <OzPill variant="acc">accent</OzPill>
            <OzPill variant="ok">ok</OzPill>
            <OzPill variant="warn">warning</OzPill>
            <OzPill variant="err">error</OzPill>
          </div>
        </OzCard>

        {/* OzStatusDot */}
        <OzCard>
          <h2 className="mb-4 font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
            OzStatusDot
          </h2>
          <div className="space-y-2 text-[13px]">
            <div className="flex items-center gap-3">
              <OzStatusDot status="on" />
              <span>Online · with halo</span>
            </div>
            <div className="flex items-center gap-3">
              <OzStatusDot status="warn" />
              <span>Degraded · with halo</span>
            </div>
            <div className="flex items-center gap-3">
              <OzStatusDot status="off" />
              <span className="text-oz2-text-muted">Offline · no halo</span>
            </div>
          </div>
        </OzCard>

        {/* Token spectrum */}
        <OzCard>
          <h2 className="mb-4 font-mono text-[11px] uppercase tracking-widest text-oz2-text-faint">
            Surface + text spectrum
          </h2>
          <div className="grid grid-cols-3 gap-3 text-[12px]">
            <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-bg p-3">
              bg
            </div>
            <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-bg-elev p-3">
              bg-elev
            </div>
            <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-bg-soft p-3">
              bg-soft
            </div>
            <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-bg-sunken p-3">
              bg-sunken
            </div>
            <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-surface p-3">
              surface
            </div>
            <div className="rounded-oz2-input border border-oz2-border-soft bg-oz2-surface-2 p-3">
              surface-2
            </div>
          </div>
          <div className="mt-4 grid grid-cols-2 gap-3 text-[12px]">
            <div className="text-oz2-text">text — primary ink</div>
            <div className="text-oz2-text-2">text-2 — secondary</div>
            <div className="text-oz2-text-muted">text-muted — tertiary</div>
            <div className="text-oz2-text-faint">text-faint — hints</div>
          </div>
        </OzCard>
      </div>
    </div>
  );
}
