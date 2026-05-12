"use client";

import Code from "@components/Code";
import InlineLink from "@components/InlineLink";
import { API_ORIGIN } from "@utils/openzro";
import { ChevronDown, ExternalLinkIcon, KeyRoundIcon } from "lucide-react";
import React, { useState } from "react";
import OzCard from "@/components/v2/OzCard";

// SCIMSetupSectionV2 — v2 paint over the legacy SCIMSetupSection.
// Content is the same (no CRUD; just instructions to copy the SCIM
// endpoint URL into your IdP), only the surface flips to v2 paint:
// OzCards instead of <details> + bg-nb-gray-940, paragraph and
// help text retoned with v2 muted/faint tokens, accordion sections
// rebuilt as collapsible OzCards with a chevron toggle.

const PROVIDER_GUIDES: ProviderGuide[] = [
  {
    name: "Okta",
    title: "Okta — Provisioning configuration",
    steps: (baseURL) => [
      <>
        In the Okta admin console, go to <b>Applications</b> → your openZro
        app → <b>Provisioning</b> → <b>Integration</b>.
      </>,
      <>
        <b>Enable API integration</b>. Set <b>Base URL</b> to{" "}
        <code className="font-mono text-[12px]">{baseURL}</code>.
      </>,
      <>
        Set <b>API Token</b> to your PAT (
        <code className="font-mono text-[12px]">nbp_...</code>).
      </>,
      <>
        Click <b>Test API Credentials</b>. Save.
      </>,
      <>
        Under <b>To App</b>, enable <i>Create Users</i>,{" "}
        <i>Update User Attributes</i>, <i>Deactivate Users</i>.
      </>,
    ],
  },
  {
    name: "Microsoft Entra",
    title: "Microsoft Entra (Azure AD) — Provisioning configuration",
    steps: (baseURL) => [
      <>
        <b>Enterprise applications</b> → your openZro app →{" "}
        <b>Provisioning</b>.
      </>,
      <>
        <b>Provisioning Mode</b> = <b>Automatic</b>.
      </>,
      <>
        <b>Tenant URL</b>:{" "}
        <code className="font-mono text-[12px]">{baseURL}</code>.
      </>,
      <>
        <b>Secret Token</b>: your PAT.
      </>,
      <>
        <b>Test Connection</b>, save, set <b>Provisioning Status</b> to{" "}
        <b>On</b>.
      </>,
    ],
  },
  {
    name: "JumpCloud / Authentik / others",
    title: "JumpCloud, Authentik, others",
    body: (baseURL) => (
      <>
        <p className="text-[13px] leading-[1.55] text-oz2-text-2">
          Any SCIM 2.0-compliant IdP works. Use{" "}
          <code className="font-mono text-[12px]">{baseURL}</code> as the SCIM
          endpoint and the PAT as the bearer token.
        </p>
        <InlineLink
          href={`${baseURL}/ServiceProviderConfig`}
          target="_blank"
          className="mt-2"
        >
          View ServiceProviderConfig <ExternalLinkIcon size={12} />
        </InlineLink>
      </>
    ),
  },
];

interface ProviderGuide {
  name: string;
  title: string;
  steps?: (baseURL: string) => React.ReactNode[];
  body?: (baseURL: string) => React.ReactNode;
}

export default function SCIMSetupSectionV2() {
  const baseURL = API_ORIGIN
    ? `${API_ORIGIN.replace(/\/+$/, "")}/scim/v2`
    : "https://your-management.example.com/scim/v2";

  return (
    <div className="flex flex-col gap-5">
      <div className="text-[13.5px] leading-[1.55] text-oz2-text-muted">
        Connect your enterprise IdP (Okta, Microsoft Entra, JumpCloud,
        Authentik, …) to auto-provision Users and Groups into openZro.
        Membership in a SCIM group becomes the user&apos;s AutoGroups list.
      </div>
      <div className="text-[12.5px] leading-[1.5] text-oz2-text-faint">
        SCIM-provisioned users carry an{" "}
        <code className="font-mono text-[12px]">issued = integration</code>{" "}
        marker. Edits made through the dashboard to those users will be
        overwritten on the next sync from the IdP — that&apos;s the
        IdP-as-source-of-truth contract, intentional.
      </div>

      <OzCard className="p-5">
        <label className="font-mono text-[10.5px] uppercase tracking-wider text-oz2-text-faint">
          Tenant URL
        </label>
        <div className="mt-2">
          <Code message="Copied!">{baseURL}</Code>
        </div>
      </OzCard>

      <OzCard className="p-5">
        <label className="font-mono text-[10.5px] uppercase tracking-wider text-oz2-text-faint">
          Authentication
        </label>
        <p className="mt-2 text-[13px] leading-[1.55] text-oz2-text-2">
          Bearer token — issue a Personal Access Token to a service user with
          the <b>admin</b> or <b>owner</b> role and paste the token into your
          IdP&apos;s SCIM connector.
        </p>
        <InlineLink href="/team/service-users" className="mt-2">
          <KeyRoundIcon size={12} /> Manage service users &amp; tokens
        </InlineLink>
      </OzCard>

      <div className="flex flex-col gap-3">
        {PROVIDER_GUIDES.map((g) => (
          <ProviderAccordion key={g.name} guide={g} baseURL={baseURL} />
        ))}
      </div>
    </div>
  );
}

function ProviderAccordion({
  guide,
  baseURL,
}: {
  guide: ProviderGuide;
  baseURL: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <OzCard flush>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-full items-center justify-between gap-3 px-5 py-3.5 text-left text-[13px] font-medium text-oz2-text transition-colors hover:bg-oz2-hover"
      >
        <span>{guide.title}</span>
        <ChevronDown
          size={14}
          className={
            "shrink-0 text-oz2-text-faint transition-transform " +
            (open ? "rotate-180" : "")
          }
        />
      </button>
      {open && (
        <div className="border-t border-oz2-border-soft px-5 py-4">
          {guide.steps && (
            <ol className="list-decimal space-y-1.5 pl-5 text-[13px] leading-[1.55] text-oz2-text-2">
              {guide.steps(baseURL).map((step, i) => (
                <li key={i}>{step}</li>
              ))}
            </ol>
          )}
          {guide.body?.(baseURL)}
        </div>
      )}
    </OzCard>
  );
}
