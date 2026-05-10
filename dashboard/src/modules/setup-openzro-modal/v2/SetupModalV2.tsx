"use client";

import { ModalContent } from "@components/modal/Modal";
import { getOpenzroUpCommand } from "@utils/openzro";
import classNames from "classnames";
import { ExternalLinkIcon, TerminalSquareIcon } from "lucide-react";
import { usePathname } from "next/navigation";
import React, { useMemo, useState } from "react";
import useCopyToClipboard from "@/hooks/useCopyToClipboard";
import { useLocalStorage } from "@/hooks/useLocalStorage";
import useOperatingSystem from "@/hooks/useOperatingSystem";
import { OperatingSystem } from "@/interfaces/OperatingSystem";

// SetupModalV2 — Notion/Arc-flavored install modal matching the
// Claude Design handoff (`install-modal-v1.bundle.html`). Replaces
// the legacy SetupModal's chrome with v2 paint and a focused
// step-list + copy pattern. OS-specific install commands come
// from the same `getOpenzroUpCommand` util the legacy tabs use,
// so the truth-of-record stays in one place.

type OS = "linux" | "windows" | "macos" | "docker";

const OS_LABEL: Record<OS, string> = {
  linux: "Linux",
  windows: "Windows",
  macos: "macOS",
  docker: "Docker",
};

interface OidcUserInfo {
  given_name?: string;
}

interface Props {
  user?: OidcUserInfo;
  setupKey?: string;
  hostname?: string;
}

export default function SetupModalV2({ user, setupKey, hostname }: Props) {
  const detected = useOperatingSystem();
  const [os, setOs] = useState<OS>(initialOS(detected));
  const [isFirstRun] = useLocalStorage<boolean>("openzro-first-run", true);
  const pathname = usePathname();
  const isInstallPage = pathname === "/install";

  const title = useMemo(() => {
    if (isFirstRun && !isInstallPage) {
      const name = user?.given_name || "there";
      return `Hello ${name} — let's add your first device`;
    }
    return setupKey ? "Install Openzro with Setup Key" : "Install Openzro";
  }, [isFirstRun, isInstallPage, setupKey, user?.given_name]);

  return (
    <ModalContent
      showClose
      maxWidthClass="sm:max-w-[540px]"
      className="overflow-hidden rounded-[18px] border border-oz2-border bg-oz2-surface p-0 shadow-oz2-lg"
    >
      <Hero title={title} setupKey={!!setupKey} />
      <OSTabs value={os} onChange={setOs} />
      <Steps os={os} setupKey={setupKey} hostname={hostname} />
      <ManualHint os={os} />
      <Footer />
    </ModalContent>
  );
}

// ─── Hero ──────────────────────────────────────────────────────────────────

function Hero({ title, setupKey }: { title: string; setupKey: boolean }) {
  return (
    <div className="px-8 pb-5 pt-9 text-center">
      <div
        className="mx-auto mb-3.5 grid h-11 w-11 place-items-center rounded-[12px] text-white shadow-oz2-acc"
        style={{
          background: "linear-gradient(135deg, #8b5cf6 0%, #4c1d95 100%)",
        }}
      >
        <svg
          viewBox="0 0 24 24"
          width={22}
          height={22}
          fill="none"
          stroke="currentColor"
          strokeWidth={2.2}
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <rect x={3} y={4} width={18} height={14} rx={3} />
          <path d="M7 10l3 3-3 3" />
          <path d="M13 16h4" />
        </svg>
      </div>
      <h2 className="text-[22px] font-semibold tracking-tight text-oz2-text">
        {title}
      </h2>
      <p className="mx-auto mt-1.5 max-w-[380px] text-[13.5px] leading-[1.5] text-oz2-text-muted">
        {setupKey
          ? "To get started, install and run Openzro with the setup key as a parameter."
          : "To get started, install Openzro and log in with your email account."}
      </p>
    </div>
  );
}

// ─── OS pill segmented ─────────────────────────────────────────────────────

function OSTabs({ value, onChange }: { value: OS; onChange: (os: OS) => void }) {
  const tabs: { id: OS; icon: React.ReactNode }[] = [
    { id: "linux", icon: <TerminalIcon /> },
    { id: "windows", icon: <WindowsIcon /> },
    { id: "macos", icon: <AppleIcon /> },
    { id: "docker", icon: <DockerIcon /> },
  ];
  return (
    <div className="mx-8 mt-1 flex gap-1 rounded-[12px] border border-oz2-border-soft bg-oz2-bg-soft p-1">
      {tabs.map((t) => {
        const active = value === t.id;
        return (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={active}
            onClick={() => onChange(t.id)}
            className={classNames(
              "flex flex-1 items-center justify-center gap-1.5 rounded-[9px] border px-2 py-2.5 text-[12.5px] font-medium transition-colors",
              active
                ? "border-oz2-border bg-oz2-surface text-oz2-text shadow-oz2-sm"
                : "border-transparent text-oz2-text-muted hover:text-oz2-text",
            )}
          >
            <span className={active ? "text-oz2-acc" : ""}>{t.icon}</span>
            {OS_LABEL[t.id]}
          </button>
        );
      })}
    </div>
  );
}

// ─── Step list ─────────────────────────────────────────────────────────────

function Steps({
  os,
  setupKey,
  hostname,
}: {
  os: OS;
  setupKey?: string;
  hostname?: string;
}) {
  const steps = buildSteps(os, setupKey, hostname);
  return (
    <div className="px-8 pb-1 pt-5">
      <p className="mb-4 flex items-center gap-2.5 text-[13.5px] font-semibold text-oz2-text">
        <span className="grid h-[26px] w-[26px] place-items-center rounded-[7px] bg-oz2-acc-soft text-oz2-acc-text">
          <TerminalSquareIcon size={14} />
        </span>
        Install via command line
      </p>
      <ol className="space-y-3.5">
        {steps.map((s, i) => (
          <Step
            key={i}
            n={i + 1}
            isLast={i === steps.length - 1}
            label={s.label}
            command={s.command}
            message={s.message}
          />
        ))}
      </ol>
    </div>
  );
}

function Step({
  n,
  isLast,
  label,
  command,
  message,
}: {
  n: number;
  isLast: boolean;
  label: React.ReactNode;
  command: React.ReactNode;
  message: string;
}) {
  return (
    <li className="relative grid grid-cols-[22px_1fr] gap-3.5">
      <span className="z-[1] mt-1 grid h-[22px] w-[22px] place-items-center rounded-full border border-oz2-border-strong bg-oz2-surface font-mono text-[11px] font-semibold text-oz2-text-muted">
        {n}
      </span>
      {!isLast && (
        <span
          aria-hidden="true"
          className="absolute left-[10px] top-[32px] bottom-[-14px] w-px bg-oz2-border"
        />
      )}
      <div className="min-w-0">
        <p className="mb-2 text-[13px] leading-[1.45] text-oz2-text-2">
          {label}
        </p>
        <CodeBlock command={command} message={message} />
      </div>
    </li>
  );
}

// ─── Code block + copy ────────────────────────────────────────────────────

function CodeBlock({
  command,
  message,
}: {
  command: React.ReactNode;
  message: string;
}) {
  const text = typeof command === "string" ? command : extractText(command);
  const [, copy, copied] = useCopyToClipboard(text);
  return (
    <div className="flex items-center gap-2.5 rounded-[10px] border border-oz2-border-soft bg-oz2-bg-soft px-3.5 py-2.5 font-mono text-[12.5px] font-medium text-oz2-text">
      <span className="select-none text-oz2-text-faint">$</span>
      <div className="oz-scroll flex-1 overflow-x-auto whitespace-nowrap">
        {command}
      </div>
      <button
        type="button"
        aria-label={copied ? "Copied" : "Copy"}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          void copy(message);
        }}
        className={classNames(
          "grid h-[30px] w-[30px] shrink-0 cursor-pointer place-items-center rounded-[8px] border border-oz2-border bg-oz2-surface transition-colors",
          copied
            ? "text-oz2-ok"
            : "text-oz2-text-muted hover:border-oz2-border-strong hover:bg-oz2-surface-2 hover:text-oz2-text",
        )}
      >
        {copied ? <CheckIcon /> : <CopyIcon />}
      </button>
    </div>
  );
}

// ─── Manual hint ──────────────────────────────────────────────────────────

function ManualHint({ os }: { os: OS }) {
  const url = `https://docs.openzro.io/how-to/getting-started#${os}`;
  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      className="mx-8 mt-3 flex items-center gap-3 border-t border-oz2-border py-4"
    >
      <span className="grid h-[22px] w-[22px] place-items-center text-oz2-text-muted">
        <ChevronIcon />
      </span>
      <span className="grid h-[28px] w-[28px] place-items-center rounded-[8px] bg-oz2-bg-soft text-oz2-text-2">
        <PackageIcon />
      </span>
      <span className="text-[13px] font-medium text-oz2-text">
        Install manually on {OS_LABEL[os]}
      </span>
    </a>
  );
}

// ─── Footer ───────────────────────────────────────────────────────────────

function Footer() {
  return (
    <div className="border-t border-oz2-border bg-oz2-bg-soft px-8 py-4 text-[12.5px] leading-[1.55] text-oz2-text-muted">
      Once connected you can add more devices or manage the network in the admin
      panel. Got questions? See our{" "}
      <a
        href="https://docs.openzro.io/how-to/getting-started#installation"
        target="_blank"
        rel="noopener noreferrer"
        className="font-medium text-oz2-acc-text underline-offset-2 hover:underline"
      >
        Installation Guide <ExternalLinkIcon size={11} className="inline" />
      </a>
      .
    </div>
  );
}

// ─── Step content per OS ──────────────────────────────────────────────────

interface StepDef {
  label: React.ReactNode;
  command: React.ReactNode;
  message: string;
}

function buildSteps(
  os: OS,
  setupKey?: string,
  hostname?: string,
): StepDef[] {
  const upCommand = (
    <>
      {getOpenzroUpCommand()}
      {setupKey && (
        <>
          {" "}
          <span className="text-oz2-acc-text">--setup-key</span>{" "}
          <span className="text-oz2-warn">{setupKey}</span>
        </>
      )}
      {hostname && (
        <>
          {" "}
          <span className="text-oz2-acc-text">--hostname</span>{" "}
          <span className="text-oz2-warn">&apos;{hostname}&apos;</span>
        </>
      )}
    </>
  );
  const upPlain = `${getOpenzroUpCommand()}${
    setupKey ? ` --setup-key ${setupKey}` : ""
  }${hostname ? ` --hostname '${hostname}'` : ""}`;

  switch (os) {
    case "linux":
      return [
        {
          label: (
            <>
              Paste this in your terminal — Openzro is detected and validated.
            </>
          ),
          command: (
            <>
              curl <span className="text-oz2-acc-text">-fsSL</span>{" "}
              <span className="text-oz2-warn">
                https://pkg.openzro.io/install.sh
              </span>{" "}
              <span className="text-oz2-text-faint">|</span> sh
            </>
          ),
          message: "Install command copied to your clipboard",
        },
        {
          label: (
            <>
              <strong className="font-medium text-oz2-text">Run Openzro</strong>{" "}
              {!setupKey && "and log in via the browser."}
            </>
          ),
          command: upCommand,
          message: `${upPlain} copied to your clipboard`,
        },
      ];
    case "macos":
      return [
        {
          label: <>Install via Homebrew tap.</>,
          command: (
            <>
              brew install{" "}
              <span className="text-oz2-warn">openzro/tap/openzro</span>
            </>
          ),
          message: "brew install command copied to your clipboard",
        },
        {
          label: (
            <>
              <strong className="font-medium text-oz2-text">Run Openzro</strong>{" "}
              {!setupKey && "and log in via the browser."}
            </>
          ),
          command: upCommand,
          message: `${upPlain} copied to your clipboard`,
        },
      ];
    case "windows":
      return [
        {
          label: (
            <>
              Download the installer from{" "}
              <a
                href="https://pkg.openzro.io/windows/openzro.msi"
                className="font-medium text-oz2-acc-text hover:underline"
                target="_blank"
                rel="noopener noreferrer"
              >
                pkg.openzro.io/windows/openzro.msi
              </a>{" "}
              and run it.
            </>
          ),
          command: (
            <>
              winget install{" "}
              <span className="text-oz2-warn">Openzro.Openzro</span>
            </>
          ),
          message: "winget install command copied to your clipboard",
        },
        {
          label: (
            <>
              <strong className="font-medium text-oz2-text">Run Openzro</strong>{" "}
              and log in via the browser.
            </>
          ),
          command: upCommand,
          message: `${upPlain} copied to your clipboard`,
        },
      ];
    case "docker":
      return [
        {
          label: (
            <>
              Run a containerized peer with{" "}
              <strong className="font-medium text-oz2-text">--rm</strong> for a
              quick test.
            </>
          ),
          command: (
            <>
              docker run <span className="text-oz2-acc-text">--rm</span>{" "}
              <span className="text-oz2-acc-text">--cap-add</span>{" "}
              <span className="text-oz2-warn">NET_ADMIN</span>{" "}
              <span className="text-oz2-acc-text">-d</span>{" "}
              <span className="text-oz2-warn">openzro/openzro:latest</span>
            </>
          ),
          message: "docker run command copied to your clipboard",
        },
        {
          label: (
            <>
              Open a shell in the container and{" "}
              <strong className="font-medium text-oz2-text">log in</strong>.
            </>
          ),
          command: (
            <>
              docker exec <span className="text-oz2-acc-text">-it</span>{" "}
              <span className="text-oz2-warn">openzro</span> {upPlain}
            </>
          ),
          message: "docker exec command copied to your clipboard",
        },
      ];
  }
}

// ─── Helpers ──────────────────────────────────────────────────────────────

function initialOS(detected: OperatingSystem): OS {
  switch (detected) {
    case OperatingSystem.WINDOWS:
      return "windows";
    case OperatingSystem.APPLE:
      return "macos";
    case OperatingSystem.IOS:
    case OperatingSystem.ANDROID:
    case OperatingSystem.LINUX:
      return "linux";
    default:
      return "linux";
  }
}

function extractText(node: React.ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(extractText).join("");
  if (React.isValidElement<{ children?: React.ReactNode }>(node)) {
    return extractText(node.props.children);
  }
  return "";
}

// ─── Inline icons (kept local so the modal is self-contained) ─────────────

function TerminalIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      width={14}
      height={14}
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <polyline points="6 9 9 12 6 15" />
      <line x1={13} y1={15} x2={18} y2={15} />
      <rect x={3} y={4} width={18} height={16} rx={2} />
    </svg>
  );
}

function WindowsIcon() {
  return (
    <svg viewBox="0 0 24 24" width={14} height={14} fill="currentColor">
      <rect x={2} y={3} width={9} height={9} />
      <rect x={13} y={3} width={9} height={9} />
      <rect x={2} y={14} width={9} height={9} />
      <rect x={13} y={14} width={9} height={9} />
    </svg>
  );
}

function AppleIcon() {
  return (
    <svg viewBox="0 0 24 24" width={14} height={14} fill="currentColor">
      <path d="M16.5 12.5c0-2.4 2-3.6 2.1-3.6-1.1-1.7-2.9-2-3.5-2-1.5-.1-2.9.9-3.7.9-.8 0-1.9-.9-3.2-.8-1.6 0-3.2 1-4 2.5-1.7 3-.4 7.4 1.2 9.8.8 1.2 1.7 2.5 3 2.5 1.2 0 1.7-.8 3.1-.8s1.9.8 3.2.8c1.3 0 2.2-1.2 3-2.4.9-1.4 1.3-2.7 1.4-2.8-.1 0-2.6-1-2.6-3.9zM14.4 5c.7-.8 1.1-2 1-3.2-1 .1-2.2.7-2.9 1.6-.6.8-1.2 2-1 3.2 1.1.1 2.2-.6 2.9-1.6z" />
    </svg>
  );
}

function DockerIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      width={14}
      height={14}
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <rect x={3} y={9} width={4} height={4} />
      <rect x={8} y={9} width={4} height={4} />
      <rect x={13} y={9} width={4} height={4} />
      <rect x={8} y={4} width={4} height={4} />
      <path d="M19 9c2 0 2 5-2 5" />
      <path d="M3 13c0 4 4 6 9 6 6 0 8-3 9-7" />
    </svg>
  );
}

function CopyIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      width={13}
      height={13}
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <rect x={9} y={9} width={11} height={11} rx={2} />
      <path d="M5 15V5a2 2 0 0 1 2-2h10" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      width={13}
      height={13}
      fill="none"
      stroke="currentColor"
      strokeWidth={3}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M4 12l6 6 10-12" />
    </svg>
  );
}

function ChevronIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      width={12}
      height={12}
      fill="none"
      stroke="currentColor"
      strokeWidth={2.2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <polyline points="9 6 15 12 9 18" />
    </svg>
  );
}

function PackageIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      width={14}
      height={14}
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M21 12a9 9 0 1 1-3-6.7L21 8" />
      <polyline points="21 3 21 8 16 8" />
    </svg>
  );
}
