"use client";

import { Modal, ModalContent, ModalTrigger } from "@components/modal/Modal";
import classNames from "classnames";
import { ExternalLinkIcon, Globe, ShieldCheck } from "lucide-react";
import React, { useMemo, useState } from "react";
import OzInput from "@/components/v2/OzInput";
import { NameserverGroup, NameserverPresets } from "@/interfaces/Nameserver";
import NameserverModal from "@/modules/dns-nameservers/NameserverModal";

type Props = {
  children: React.ReactNode;
};

// NameserverTemplateModal — v2 paint per the handoff "DNS · novo
// nameserver" prototype. Search field + 2-col grid of provider tiles,
// each with a tinted glyph (mono initial letter), tag chip, short
// description, and the upstream IPs inline. Custom DNS spans both
// columns at the bottom as a globe-glyph tile with a + badge.
//
// UX is preserved: clicking a tile commits the selection and pops the
// next NameserverModal — no separate Continue step. Search filters
// providers as the operator types.

export default function NameserverTemplateModal({ children }: Readonly<Props>) {
  const [open, setOpen] = useState(false);
  const [presetModal, setPresetModal] = useState(false);
  const [preset, setPreset] = useState(NameserverPresets.Default);

  return (
    <>
      <Modal open={open} onOpenChange={setOpen} key={open ? 1 : 0}>
        <ModalTrigger asChild={true}>{children}</ModalTrigger>
        <NameserverTemplateModalContent
          onePresetSelection={(p) => {
            setPreset(p);
            setPresetModal(true);
          }}
        />
      </Modal>
      {preset && presetModal && (
        <NameserverModal
          open={presetModal}
          onOpenChange={(o) => {
            setPresetModal(o);
            if (!o) setOpen(false);
          }}
          preset={preset}
        />
      )}
    </>
  );
}

type ModalProps = {
  onePresetSelection: (preset: NameserverGroup) => void;
};

interface ProviderTile {
  initial: string;
  name: string;
  tag: string;
  tagAccent?: boolean;
  description: string;
  ips: string[];
  href: string;
  tone: "violet" | "amber" | "emerald" | "sky" | "rose";
  preset: NameserverGroup;
}

const PROVIDERS: ProviderTile[] = [
  {
    initial: "G",
    name: "Google DNS",
    tag: "public",
    description:
      "Free global resolver from Google. Focus on performance, security and compliance. Good baseline for most teams.",
    ips: ["8.8.8.8", "8.8.4.4"],
    href: "https://developers.google.com/speed/public-dns",
    tone: "sky",
    preset: NameserverPresets.Google,
  },
  {
    initial: "CF",
    name: "Cloudflare DNS",
    tag: "recommended",
    tagAccent: true,
    description:
      "Enterprise-grade DNS with the fastest response time, massive redundancy and built-in DDoS mitigation. DNSSEC by default.",
    ips: ["1.1.1.1", "1.0.0.1"],
    href: "https://www.cloudflare.com/learning/dns/what-is-1.1.1.1/",
    tone: "amber",
    preset: NameserverPresets.Cloudflare,
  },
  {
    initial: "EU",
    name: "DNS0.EU",
    tag: "sovereign",
    description:
      "Sovereign, GDPR-compliant resolver focused on protecting EU citizens and organisations. No logs.",
    ips: ["193.110.81.0", "185.253.5.0"],
    href: "https://www.dns0.eu/",
    tone: "violet",
    preset: NameserverPresets.DNS0,
  },
  {
    initial: "0",
    name: "DNS0.EU Zero",
    tag: "zero-trust",
    description:
      "Boosts the catch-rate of malicious domains by combining vetted threat intel with heuristics on high-risk patterns.",
    ips: ["193.110.81.9", "185.253.5.9"],
    href: "https://www.dns0.eu/zero",
    tone: "rose",
    preset: NameserverPresets.DNS0Zero,
  },
  {
    initial: "Q9",
    name: "Quad9",
    tag: "threat-filter",
    description:
      "Operated by the Swiss Quad9 Foundation — public DNS with malicious-domain blocking via collective threat intel.",
    ips: ["9.9.9.9", "149.112.112.112"],
    href: "https://quad9.net/",
    tone: "emerald",
    preset: NameserverPresets.Quad9,
  },
];

const TONE_GLYPH: Record<ProviderTile["tone"], string> = {
  violet: "bg-oz2-acc-soft text-oz2-acc-text",
  amber: "bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300",
  emerald:
    "bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300",
  sky: "bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300",
  rose: "bg-rose-100 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300",
};

export function NameserverTemplateModalContent({
  onePresetSelection,
}: Readonly<ModalProps>) {
  const [search, setSearch] = useState("");

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return PROVIDERS;
    return PROVIDERS.filter((p) =>
      [p.name, p.tag, p.description, ...p.ips]
        .join(" ")
        .toLowerCase()
        .includes(q),
    );
  }, [search]);

  return (
    <ModalContent maxWidthClass={"max-w-3xl"} showClose={true}>
      <header className="border-b border-oz2-border-soft px-6 py-5">
        <div className="flex items-center gap-2 font-mono text-[10.5px] uppercase tracking-[0.06em] text-oz2-acc-text">
          <span className="inline-block h-1.5 w-1.5 rounded-full bg-oz2-acc" />
          DNS · new nameserver
        </div>
        <h2 className="mt-1.5 text-[18px] font-semibold tracking-tight text-oz2-text">
          Select a provider
        </h2>
        <p className="mt-1 max-w-xl text-[12.5px] text-oz2-text-muted">
          Public resolvers run upstream of your mesh. Pick a preset to seed
          the configuration, or roll your own with Custom DNS.
        </p>
      </header>

      <div className="px-6 py-3">
        <OzInput
          prefix={
            <svg
              viewBox="0 0 24 24"
              width="14"
              height="14"
              fill="none"
              stroke="currentColor"
              strokeWidth={1.7}
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <circle cx={11} cy={11} r={7} />
              <path d="m20 20-3.5-3.5" />
            </svg>
          }
          placeholder="Search provider — e.g. cloudflare, quad9, gdpr…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          autoFocus
        />
      </div>

      <div className="grid grid-cols-1 gap-2.5 px-6 pb-5 md:grid-cols-2">
        {filtered.map((p) => (
          <ProviderButton
            key={p.name}
            tile={p}
            onClick={() => onePresetSelection(p.preset)}
          />
        ))}
        {filtered.length > 0 && (
          <CustomDNSTile
            onClick={() => onePresetSelection(NameserverPresets.Default)}
          />
        )}
        {filtered.length === 0 && (
          <div className="rounded-oz2-card border border-dashed border-oz2-border bg-oz2-bg-sunken/40 px-4 py-6 text-center text-[12.5px] text-oz2-text-muted md:col-span-2">
            No provider matches “{search}”. Pick Custom DNS below to enter
            your own.{" "}
            <button
              type="button"
              onClick={() => onePresetSelection(NameserverPresets.Default)}
              className="ml-1 font-semibold text-oz2-acc-text hover:underline"
            >
              Use Custom DNS →
            </button>
          </div>
        )}
      </div>

      <footer className="flex items-center justify-between gap-3 border-t border-oz2-border-soft bg-oz2-bg-sunken/60 px-6 py-3 text-[12px] text-oz2-text-muted">
        <span className="inline-flex items-center gap-2">
          <ShieldCheck size={13} className="text-oz2-text-faint" />
          openZro does not store DNS queries — the resolver runs in your
          mesh.
        </span>
      </footer>
    </ModalContent>
  );
}

function ProviderButton({
  tile,
  onClick,
}: {
  tile: ProviderTile;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="group grid grid-cols-[56px_minmax(0,1fr)] gap-3.5 rounded-oz2-card border border-transparent bg-transparent p-3.5 text-left transition-all hover:border-oz2-border hover:bg-oz2-bg-sunken focus-visible:outline-none focus-visible:border-oz2-acc focus-visible:ring-2 focus-visible:ring-oz2-acc/30"
    >
      <div
        aria-hidden
        className={classNames(
          "grid h-[56px] w-[56px] place-items-center rounded-[14px] border border-oz2-border-soft font-mono text-[18px] font-bold tracking-tight",
          TONE_GLYPH[tile.tone],
        )}
      >
        {tile.initial}
      </div>
      <div className="flex min-w-0 flex-col gap-1 pt-0.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[14px] font-semibold text-oz2-text">
            {tile.name}
          </span>
          <span
            className={classNames(
              "rounded-full px-2 py-0.5 font-mono text-[10px] uppercase tracking-wider",
              tile.tagAccent
                ? "bg-oz2-acc-soft text-oz2-acc-text"
                : "border border-oz2-border-soft bg-oz2-bg-sunken text-oz2-text-muted",
            )}
          >
            {tile.tag}
          </span>
        </div>
        <p className="line-clamp-2 text-[12.5px] leading-[1.45] text-oz2-text-muted">
          {tile.description}
        </p>
        <div className="mt-1.5 flex items-center gap-2 font-mono text-[11px] text-oz2-text-faint">
          <span className="text-oz2-text-2">{tile.ips[0]}</span>
          {tile.ips[1] && (
            <>
              <span className="inline-block h-[3px] w-[3px] rounded-full bg-oz2-text-faint/60" />
              <span>{tile.ips[1]}</span>
            </>
          )}
          <a
            href={tile.href}
            target="_blank"
            rel="noopener noreferrer"
            onClick={(e) => e.stopPropagation()}
            className="ml-auto inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
          >
            Learn more
            <ExternalLinkIcon size={10} />
          </a>
        </div>
      </div>
    </button>
  );
}

function CustomDNSTile({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="group grid grid-cols-[56px_minmax(0,1fr)] gap-3.5 rounded-oz2-card border border-dashed border-oz2-border bg-transparent p-3.5 text-left transition-all hover:border-oz2-acc hover:bg-oz2-acc-soft/30 focus-visible:outline-none focus-visible:border-oz2-acc focus-visible:ring-2 focus-visible:ring-oz2-acc/30"
    >
      <div
        aria-hidden
        className="relative grid h-[56px] w-[56px] place-items-center rounded-[14px] border border-oz2-border-soft bg-oz2-bg-sunken text-oz2-text-2"
      >
        <Globe size={24} />
        <span className="absolute -bottom-1 -right-1 grid h-4 w-4 place-items-center rounded-full border border-oz2-border bg-oz2-surface text-oz2-text-faint">
          <svg
            viewBox="0 0 24 24"
            width="9"
            height="9"
            fill="none"
            stroke="currentColor"
            strokeWidth={3}
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <line x1={12} y1={5} x2={12} y2={19} />
            <line x1={5} y1={12} x2={19} y2={12} />
          </svg>
        </span>
      </div>
      <div className="flex min-w-0 flex-col gap-1 pt-0.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[14px] font-semibold text-oz2-text">
            Custom DNS
          </span>
          <span className="rounded-full border border-oz2-border-soft bg-oz2-bg-sunken px-2 py-0.5 font-mono text-[10px] uppercase tracking-wider text-oz2-text-muted">
            any IP
          </span>
        </div>
        <p className="text-[12.5px] leading-[1.45] text-oz2-text-muted">
          Use your own nameservers — a public DNS of your choice or a private
          resolver inside your network.
        </p>
        <div className="mt-1.5 flex items-center gap-2 font-mono text-[11px] text-oz2-text-faint">
          <span className="text-oz2-text-2">e.g. 10.20.0.53</span>
          <span className="inline-block h-[3px] w-[3px] rounded-full bg-oz2-text-faint/60" />
          <span>+ matching domains</span>
        </div>
      </div>
    </button>
  );
}
