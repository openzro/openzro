"use client";

import { ModalContent } from "@components/modal/Modal";
import { getOpenzroUpCommand, GRPC_API_ORIGIN } from "@utils/openzro";
import classNames from "classnames";
import {
  DownloadIcon,
  ExternalLinkIcon,
  TerminalSquareIcon,
} from "lucide-react";
import { usePathname } from "next/navigation";
import React, { useMemo, useState } from "react";
import useCopyToClipboard from "@/hooks/useCopyToClipboard";
import { useLatestRelease } from "@/hooks/useLatestRelease";
import { useLocalStorage } from "@/hooks/useLocalStorage";
import useOperatingSystem from "@/hooks/useOperatingSystem";
import { OperatingSystem } from "@/interfaces/OperatingSystem";

// SetupModalV2 — Notion/Arc-flavored install modal matching the
// Claude Design handoff (`install-modal-v1.bundle.html`). Replaces
// the legacy SetupModal's chrome (Tabs + Accordion + Steps + Modal-
// Footer) with v2 paint. Step-by-step copy and the OS-specific
// commands are kept verbatim from the legacy LinuxTab / MacOSTab /
// WindowsTab / DockerTab so the install flow stays identical — only
// the chrome is repainted.

type OS = "linux" | "windows" | "macos" | "docker";

const OS_LABEL: Record<OS, string> = {
  linux: "Linux",
  windows: "Windows",
  macos: "macOS",
  docker: "Docker",
};

const MACOS_PKG_URL = "https://pkg.openzro.io/macos/openzro.pkg";
const WINDOWS_MSI_URL = "https://pkg.openzro.io/windows/openzro.msi";

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
      maxWidthClass="sm:max-w-[640px]"
      className="overflow-hidden rounded-[18px] border border-oz2-border bg-oz2-surface p-0 shadow-oz2-lg"
    >
      <Hero title={title} setupKey={!!setupKey} />
      <OSTabs value={os} onChange={setOs} />
      <PrimarySteps os={os} setupKey={setupKey} hostname={hostname} />
      <ManualAccordion os={os} setupKey={setupKey} hostname={hostname} />
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
      <p className="mx-auto mt-1.5 max-w-[420px] text-[13.5px] leading-[1.5] text-oz2-text-muted">
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

// ─── Primary steps (per-OS, mirrors legacy texts) ─────────────────────────

function PrimarySteps({
  os,
  setupKey,
  hostname,
}: {
  os: OS;
  setupKey?: string;
  hostname?: string;
}) {
  return (
    <div className="px-8 pb-1 pt-5">
      <SectionTitle
        icon={<TerminalSquareIcon size={14} />}
        label={primaryHeading(os)}
      />
      <StepsList>
        {os === "linux" && <LinuxPrimary setupKey={setupKey} hostname={hostname} />}
        {os === "macos" && <MacOSPrimary setupKey={setupKey} hostname={hostname} />}
        {os === "windows" && <WindowsPrimary setupKey={setupKey} hostname={hostname} />}
        {os === "docker" && <DockerPrimary setupKey={setupKey} hostname={hostname} />}
      </StepsList>
    </div>
  );
}

function primaryHeading(os: OS): string {
  if (os === "linux") return "Install with Command-line";
  if (os === "docker") return "Install on Ubuntu";
  return `Install on ${OS_LABEL[os]}`;
}

// ─── Per-OS primary step content ──────────────────────────────────────────

function LinuxPrimary({
  setupKey,
  hostname,
}: {
  setupKey?: string;
  hostname?: string;
}) {
  return (
    <>
      <Step n={1}>
        <CodeBlock
          lines={[
            <>
              curl <span className="text-oz2-acc-text">-fsSL</span>{" "}
              <span className="text-oz2-warn">
                https://pkg.openzro.io/install.sh
              </span>{" "}
              <span className="text-oz2-text-faint">|</span> sh
            </>,
          ]}
          copyText="curl -fsSL https://pkg.openzro.io/install.sh | sh"
          message="Install command copied to your clipboard"
        />
      </Step>
      <Step n={2} isLast>
        <StepLabel>
          Run Openzro {!setupKey && "and log in the browser"}
        </StepLabel>
        <UpCommand setupKey={setupKey} hostname={hostname} />
      </Step>
    </>
  );
}

function MacOSPrimary({
  setupKey,
  hostname,
}: {
  setupKey?: string;
  hostname?: string;
}) {
  const release = useLatestRelease().data;
  const downloadLabel =
    release?.tag_name ? `${release.tag_name} (Installer)` : "Installer";
  const stepsAfterDownload = setupKey ? 1 : 2;
  const lastStepIndex = (GRPC_API_ORIGIN ? 3 : 2) + (setupKey ? 0 : 1);

  return (
    <>
      <Step n={1}>
        <StepLabel>Download the latest macOS build</StepLabel>
        <DownloadButtonRow href={MACOS_PKG_URL} label={`Download openZro ${downloadLabel}`} />
        <StepNote>
          Universal installer — works on both Intel and Apple Silicon. Double-click
          the .pkg, follow the prompts, and the daemon registers as a LaunchDaemon
          while the openZro UI lands in <Code>/Applications/openZro UI.app</Code>.
          On first run macOS may show a Gatekeeper warning (&quot;cannot be opened
          because Apple cannot check it&quot;) — right-click → <em>Open</em> to
          bypass once, or run{" "}
          <Code>xattr -d com.apple.quarantine ~/Downloads/openzro.pkg</Code>. Apple
          Developer ID notarization is coming soon and will eliminate the warning.
        </StepNote>
      </Step>

      {GRPC_API_ORIGIN && (
        <Step n={2}>
          <StepLabel>
            Click on &quot;Settings&quot; then &quot;Advanced Settings&quot; from the
            Openzro icon in your system tray and enter the following &quot;Management
            URL&quot;
          </StepLabel>
          <CodeBlock
            lines={[GRPC_API_ORIGIN]}
            copyText={GRPC_API_ORIGIN}
            message="Management URL copied to your clipboard"
          />
        </Step>
      )}

      {setupKey ? (
        <Step n={GRPC_API_ORIGIN ? 3 : 2} isLast>
          <StepLabel>Open Terminal and run Openzro</StepLabel>
          <UpCommand setupKey={setupKey} hostname={hostname} />
        </Step>
      ) : (
        <>
          <Step n={GRPC_API_ORIGIN ? 3 : 2}>
            <StepLabel>
              Click on &quot;Connect&quot; from the Openzro icon in your system tray
            </StepLabel>
          </Step>
          <Step n={lastStepIndex} isLast>
            <StepLabel>Sign up using your email address</StepLabel>
          </Step>
        </>
      )}
    </>
  );
}

function WindowsPrimary({
  setupKey,
  hostname,
}: {
  setupKey?: string;
  hostname?: string;
}) {
  const release = useLatestRelease().data;
  const versionLabel = release?.tag_name ? ` ${release.tag_name}` : "";
  const lastStepIndex = (GRPC_API_ORIGIN ? 3 : 2) + (setupKey ? 0 : 1);

  return (
    <>
      <Step n={1}>
        <StepLabel>Download the latest Windows build</StepLabel>
        <DownloadButtonRow
          href={WINDOWS_MSI_URL}
          label={`Download openZro${versionLabel} (Installer)`}
        />
        <StepNote>
          The .msi bundles the daemon, the system-tray UI, and the wintun driver —
          one click installs everything. Windows may show a SmartScreen warning on
          first run (click <em>More info → Run anyway</em>); EV code-signing via
          SignPath is on the way and will eliminate the prompt.
        </StepNote>
      </Step>

      {GRPC_API_ORIGIN && (
        <Step n={2}>
          <StepLabel>
            Click on &quot;Settings&quot; then &quot;Advanced Settings&quot; from the
            Openzro icon in your system tray and enter the following &quot;Management
            URL&quot;
          </StepLabel>
          <CodeBlock
            lines={[GRPC_API_ORIGIN]}
            copyText={GRPC_API_ORIGIN}
            message="Management URL copied to your clipboard"
          />
        </Step>
      )}

      {setupKey ? (
        <Step n={GRPC_API_ORIGIN ? 3 : 2} isLast>
          <StepLabel>Open Command-line and run Openzro</StepLabel>
          <UpCommand setupKey={setupKey} hostname={hostname} />
        </Step>
      ) : (
        <>
          <Step n={GRPC_API_ORIGIN ? 3 : 2}>
            <StepLabel>
              Click on &quot;Connect&quot; from the Openzro icon in your system tray
            </StepLabel>
          </Step>
          <Step n={lastStepIndex} isLast>
            <StepLabel>Sign up using your email address</StepLabel>
          </Step>
        </>
      )}
    </>
  );
}

function DockerPrimary({
  setupKey,
  hostname,
}: {
  setupKey?: string;
  hostname?: string;
}) {
  const dockerLines: React.ReactNode[] = [
    <>docker run --rm -d \</>,
    <> --cap-add=NET_ADMIN \</>,
    <>
      {" "}-e OZ_SETUP_KEY=
      <span className="text-oz2-warn">{setupKey ?? "SETUP_KEY"}</span> \
    </>,
  ];
  if (hostname) {
    dockerLines.push(
      <>
        {" "}-e OZ_HOSTNAME=
        <span className="text-oz2-warn">{`'${hostname}'`}</span> \
      </>,
    );
  }
  dockerLines.push(<> -v openzro-client:/var/lib/openzro \</>);
  if (GRPC_API_ORIGIN) {
    dockerLines.push(
      <>
        {" "}-e OZ_MANAGEMENT_URL=
        <span className="text-oz2-warn">{GRPC_API_ORIGIN}</span> \
      </>,
    );
  }
  dockerLines.push(<> openzro/openzro:latest</>);

  const copyText = [
    "docker run --rm -d \\",
    "  --cap-add=NET_ADMIN \\",
    `  -e OZ_SETUP_KEY=${setupKey ?? "SETUP_KEY"} \\`,
    ...(hostname ? [`  -e OZ_HOSTNAME='${hostname}' \\`] : []),
    "  -v openzro-client:/var/lib/openzro \\",
    ...(GRPC_API_ORIGIN ? [`  -e OZ_MANAGEMENT_URL=${GRPC_API_ORIGIN} \\`] : []),
    "  openzro/openzro:latest",
  ].join("\n");

  return (
    <>
      <Step n={1}>
        <StepLabel>Install Docker</StepLabel>
        <ExternalButton
          href="https://docs.docker.com/engine/install/"
          label="Official Docker Installation Guide"
        />
      </Step>
      <Step n={2}>
        <StepLabel>Run Openzro container</StepLabel>
        <CodeBlock
          lines={dockerLines}
          copyText={copyText}
          message="Docker run command copied to your clipboard"
        />
      </Step>
      <Step n={3} isLast>
        <StepLabel>Read our documentation</StepLabel>
        <a
          href="https://docs.openzro.io/how-to/installation/docker"
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1.5 text-[12.5px] font-medium text-oz2-acc-text underline-offset-2 hover:underline"
        >
          Running Openzro in Docker
          <ExternalLinkIcon size={11} />
        </a>
      </Step>
    </>
  );
}

// ─── Manual install accordion ─────────────────────────────────────────────

function ManualAccordion({
  os,
  setupKey,
  hostname,
}: {
  os: OS;
  setupKey?: string;
  hostname?: string;
}) {
  const [open, setOpen] = useState(false);

  // Legacy: only Linux + macOS expose a manual-install accordion.
  // Windows + Docker have no advanced manual section in the legacy
  // SetupModal, so we hide the row for those tabs.
  const labels: Partial<Record<OS, string>> = {
    linux: "Install manually on Ubuntu",
    macos: "Install manually with Homebrew",
  };
  const label = labels[os];
  if (!label) return null;

  return (
    <div className="mx-8 mt-3 border-t border-oz2-border">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full cursor-pointer items-center gap-3 py-4 text-left"
      >
        <span
          className={classNames(
            "grid h-[22px] w-[22px] place-items-center text-oz2-text-muted transition-transform",
            open && "rotate-90",
          )}
        >
          <ChevronIcon />
        </span>
        <span className="grid h-[28px] w-[28px] place-items-center rounded-[8px] bg-oz2-bg-soft text-oz2-text-2">
          <PackageIcon />
        </span>
        <span className="text-[13px] font-medium text-oz2-text">{label}</span>
      </button>
      {open && (
        <StepsList className="pb-4 pl-[60px] pr-1">
          {os === "linux" && (
            <LinuxManual setupKey={setupKey} hostname={hostname} />
          )}
          {os === "macos" && (
            <MacOSManualHomebrew setupKey={setupKey} hostname={hostname} />
          )}
        </StepsList>
      )}
    </div>
  );
}

function LinuxManual({
  setupKey,
  hostname,
}: {
  setupKey?: string;
  hostname?: string;
}) {
  return (
    <>
      <Step n={1}>
        <StepLabel>Add our repository</StepLabel>
        <CodeBlock
          lines={[
            <>sudo apt-get update</>,
            <>sudo apt install ca-certificates curl gnupg -y</>,
            <>
              curl -sSL https://pkg.openzro.io/openzro-archive-key.asc | sudo gpg
              --dearmor --output /usr/share/keyrings/openzro-archive-keyring.gpg
            </>,
            <>
              {`echo 'deb [signed-by=/usr/share/keyrings/openzro-archive-keyring.gpg] https://pkg.openzro.io/apt stable main' | sudo tee /etc/apt/sources.list.d/openzro.list`}
            </>,
          ]}
          copyText={[
            "sudo apt-get update",
            "sudo apt install ca-certificates curl gnupg -y",
            "curl -sSL https://pkg.openzro.io/openzro-archive-key.asc | sudo gpg --dearmor --output /usr/share/keyrings/openzro-archive-keyring.gpg",
            `echo 'deb [signed-by=/usr/share/keyrings/openzro-archive-keyring.gpg] https://pkg.openzro.io/apt stable main' | sudo tee /etc/apt/sources.list.d/openzro.list`,
          ].join("\n")}
          message="Repository setup commands copied to your clipboard"
        />
      </Step>
      <Step n={2}>
        <StepLabel>Install Openzro</StepLabel>
        <CodeBlock
          lines={[
            <>sudo apt-get update</>,
            <span className="text-oz2-text-faint"># for CLI only</span>,
            <>sudo apt-get install openzro</>,
            <span className="text-oz2-text-faint"># for GUI package</span>,
            <>sudo apt-get install openzro-ui</>,
          ]}
          copyText={[
            "sudo apt-get update",
            "sudo apt-get install openzro",
            "sudo apt-get install openzro-ui",
          ].join("\n")}
          message="Install commands copied to your clipboard"
        />
      </Step>
      <Step n={3} isLast>
        <StepLabel>
          Run Openzro {!setupKey && "and log in the browser"}
        </StepLabel>
        <UpCommand setupKey={setupKey} hostname={hostname} />
      </Step>
    </>
  );
}

function MacOSManualHomebrew({
  setupKey,
  hostname,
}: {
  setupKey?: string;
  hostname?: string;
}) {
  return (
    <>
      <Step n={1}>
        <StepLabel>Download and install HomeBrew</StepLabel>
        <ExternalButton
          href="https://brew.sh/"
          label="HomeBrew Installation Guide"
        />
      </Step>
      <Step n={2}>
        <StepLabel>Install the openzro CLI</StepLabel>
        <CodeBlock
          lines={[<>brew install openzro/tap/openzro</>]}
          copyText="brew install openzro/tap/openzro"
          message="brew install command copied to your clipboard"
        />
        <StepNote>
          For the system-tray GUI app, use the .pkg installer above instead — a
          Homebrew Cask for openzro-ui will ship once we publish the macOS .app
          bundle (tracked as part of the packaging epic).
        </StepNote>
      </Step>
      <Step n={3}>
        <StepLabel>Start Openzro daemon</StepLabel>
        <CodeBlock
          lines={[
            <span className="text-oz2-text-faint">
              # daemon needs root to manage WireGuard interfaces
            </span>,
            <>sudo brew services start openzro</>,
          ]}
          copyText="sudo brew services start openzro"
          message="brew services command copied to your clipboard"
        />
      </Step>
      <Step n={4} isLast>
        <StepLabel>
          Run Openzro {!setupKey && "and log in the browser"}
        </StepLabel>
        <UpCommand setupKey={setupKey} hostname={hostname} />
      </Step>
    </>
  );
}

// ─── Step primitives ──────────────────────────────────────────────────────

function StepsList({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <ol className={classNames("space-y-3.5", className)}>{children}</ol>
  );
}

function Step({
  n,
  isLast = false,
  children,
}: {
  n: number;
  isLast?: boolean;
  children: React.ReactNode;
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
      <div className="min-w-0">{children}</div>
    </li>
  );
}

function StepLabel({ children }: { children: React.ReactNode }) {
  return (
    <p className="mb-2 text-[13px] leading-[1.5] text-oz2-text-2">{children}</p>
  );
}

function StepNote({ children }: { children: React.ReactNode }) {
  return (
    <p className="mt-2 text-[12px] leading-[1.5] text-oz2-text-muted">
      {children}
    </p>
  );
}

function Code({ children }: { children: React.ReactNode }) {
  return (
    <code className="rounded-[5px] bg-oz2-bg-soft px-1.5 py-px font-mono text-[11.5px] font-medium text-oz2-acc-text">
      {children}
    </code>
  );
}

function SectionTitle({
  icon,
  label,
}: {
  icon: React.ReactNode;
  label: string;
}) {
  return (
    <p className="mb-4 flex items-center gap-2.5 text-[13.5px] font-semibold text-oz2-text">
      <span className="grid h-[26px] w-[26px] place-items-center rounded-[7px] bg-oz2-acc-soft text-oz2-acc-text">
        {icon}
      </span>
      {label}
    </p>
  );
}

// ─── Code block + copy ────────────────────────────────────────────────────

function CodeBlock({
  lines,
  copyText,
  message,
}: {
  lines: React.ReactNode[];
  copyText: string;
  message: string;
}) {
  const [, copy, copied] = useCopyToClipboard(copyText);
  const isMulti = lines.length > 1;
  return (
    <div
      className={classNames(
        "flex gap-2.5 rounded-[10px] border border-oz2-border-soft bg-oz2-bg-soft px-3.5 py-2.5 font-mono text-[12.5px] font-medium text-oz2-text",
        isMulti ? "items-start" : "items-center",
      )}
    >
      <span
        className={classNames(
          "select-none text-oz2-text-faint",
          isMulti && "leading-[1.6]",
        )}
      >
        $
      </span>
      <div className="oz-scroll min-w-0 flex-1 overflow-x-auto">
        {lines.map((line, i) => (
          <div
            key={i}
            className={classNames(
              "whitespace-pre",
              isMulti && "leading-[1.6]",
            )}
          >
            {line}
          </div>
        ))}
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

function UpCommand({
  setupKey,
  hostname,
}: {
  setupKey?: string;
  hostname?: string;
}) {
  const cmd = (
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
  const plain = `${getOpenzroUpCommand()}${
    setupKey ? ` --setup-key ${setupKey}` : ""
  }${hostname ? ` --hostname '${hostname}'` : ""}`;
  return (
    <CodeBlock
      lines={[cmd]}
      copyText={plain}
      message="openzro up command copied to your clipboard"
    />
  );
}

// ─── Buttons + footer ─────────────────────────────────────────────────────

function DownloadButtonRow({
  href,
  label,
}: {
  href: string;
  label: string;
}) {
  return (
    <div className="mb-2 flex flex-wrap gap-3">
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex h-[34px] items-center gap-2 rounded-[10px] border border-transparent bg-oz2-acc px-3.5 text-[13px] font-medium text-oz2-text-on-acc shadow-oz2-acc transition-colors hover:bg-oz2-acc-hover"
      >
        <DownloadIcon size={14} />
        {label}
      </a>
    </div>
  );
}

function ExternalButton({
  href,
  label,
}: {
  href: string;
  label: string;
}) {
  return (
    <div className="mb-1 flex flex-wrap gap-3">
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex h-[34px] items-center gap-2 rounded-[10px] border border-transparent bg-oz2-acc px-3.5 text-[13px] font-medium text-oz2-text-on-acc shadow-oz2-acc transition-colors hover:bg-oz2-acc-hover"
      >
        <ExternalLinkIcon size={14} />
        {label}
      </a>
    </div>
  );
}

function Footer() {
  return (
    <div className="border-t border-oz2-border bg-oz2-bg-soft px-8 py-4 text-[12.5px] leading-[1.55] text-oz2-text-muted">
      After that you should be connected. Add more devices to your network or
      manage your existing devices in the admin panel. If you have further
      questions check out our{" "}
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
