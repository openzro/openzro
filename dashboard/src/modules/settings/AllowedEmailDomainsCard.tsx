"use client";

import { AtSign, Plus, X } from "lucide-react";
import React, { useState } from "react";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzSettingsCard from "@/modules/settings/v2/OzSettingsCard";

// The server normalizes and validates each entry (ADR-0025); this
// mirrors only the normalization so the list shows what will be stored.
const MAX_DOMAINS = 100;

export function normalizeEmailDomain(value: string): string {
  return value.trim().toLowerCase().replace(/^@/, "");
}

type Props = {
  domains: string[];
  onChange: (domains: string[]) => void;
  disabled?: boolean;
};

// AllowedEmailDomainsCard edits settings.allowed_email_domains (ADR-0026
// D3). An empty list disables the check; the parent saves the account.
export default function AllowedEmailDomainsCard({
  domains,
  onChange,
  disabled = false,
}: Readonly<Props>) {
  const [draft, setDraft] = useState("");
  const candidate = normalizeEmailDomain(draft);
  const duplicate = candidate !== "" && domains.includes(candidate);
  const canAdd =
    !disabled && candidate !== "" && !duplicate && domains.length < MAX_DOMAINS;

  const add = () => {
    if (!canAdd) return;
    onChange([...domains, candidate]);
    setDraft("");
  };

  return (
    <OzSettingsCard
      title="Allowed email domains"
      sub="Only users with a verified email in one of these domains can join the account, whether on first login, through SCIM or by invite. Subdomains must be listed separately. Existing users are not affected. Leave empty to allow any domain."
    >
      <div className="flex gap-2">
        <OzInput
          prefix={<AtSign size={14} />}
          placeholder="example.com"
          value={draft}
          disabled={disabled}
          data-cy="allowed-domain-input"
          wrapperClassName="w-[280px]"
          error={duplicate ? "Already in the list." : undefined}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              add();
            }
          }}
        />
        <OzButton
          type="button"
          disabled={!canAdd}
          data-cy="allowed-domain-add"
          onClick={add}
        >
          <Plus size={14} />
          Add
        </OzButton>
      </div>

      {domains.length === 0 ? (
        <p className="text-[13px] text-oz2-text-muted">
          No restriction: users from any email domain can join.
        </p>
      ) : (
        <ul className="flex flex-wrap gap-2" data-cy="allowed-domain-list">
          {domains.map((domain) => (
            <li
              key={domain}
              className="inline-flex items-center gap-1.5 rounded-oz2-input border border-oz2-border bg-oz2-bg-sunken py-1 pl-2.5 pr-1 font-mono text-[12.5px] text-oz2-text"
            >
              {domain}
              <button
                type="button"
                aria-label={`Remove ${domain}`}
                disabled={disabled}
                onClick={() => onChange(domains.filter((d) => d !== domain))}
                className="grid h-5 w-5 place-items-center rounded text-oz2-text-faint hover:bg-oz2-hover hover:text-oz2-text disabled:opacity-40"
              >
                <X size={12} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </OzSettingsCard>
  );
}
