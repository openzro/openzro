"use client";

import Badge from "@components/Badge";
import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import cidr from "ip-cidr";
import {
  AlertTriangle,
  Check,
  PencilLine,
  PlusCircle,
  Trash2,
  X,
} from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import { useDialog } from "@/contexts/DialogProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import {
  DNSRecord,
  DNSRecordRequest,
  DNSRecordType,
  DNSZone,
} from "@/interfaces/DNSZone";

// Cloudflare-style record management for a single zone. Operators
// type only the leaf label (e.g. `www`) and the UI shows the zone
// suffix (`.zona.internal`) as a fixed adornment. `@` resolves to
// the apex (the zone domain itself) — Cloudflare convention. Submit
// always sends the full FQDN to the backend.

const FQDN_RE =
  /^(?=.{1,253}$)(?:(?!-)[A-Za-z0-9-]{1,63}(?<!-)\.)+[A-Za-z]{2,63}$/;
const LEAF_LABEL_RE = /^(?=.{1,63}$)(?!-)[A-Za-z0-9-](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.(?!-)[A-Za-z0-9-](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*$/;
const IPV4_RE =
  /^(25[0-5]|2[0-4][0-9]|[01]?[0-9]{1,2})(\.(25[0-5]|2[0-4][0-9]|[01]?[0-9]{1,2})){3}$/;

// Strip the zone-domain suffix (and a trailing dot) from a full FQDN
// so the editor can pre-populate the Name input with just the leaf
// portion. Apex records (name === zone.domain) render as `@`.
function leafFromFqdn(fqdn: string, zoneDomain: string): string {
  if (!fqdn) return "";
  if (fqdn === zoneDomain) return "@";
  const suffix = "." + zoneDomain;
  if (fqdn.endsWith(suffix)) return fqdn.slice(0, -suffix.length);
  return fqdn;
}

// Inverse: combine the leaf label the operator typed with the zone
// domain. `@` is the Cloudflare apex shorthand → return the bare zone.
function fqdnFromLeaf(leaf: string, zoneDomain: string): string {
  const trimmed = leaf.trim();
  if (trimmed === "@" || trimmed === "") return zoneDomain;
  return `${trimmed}.${zoneDomain}`;
}

function validateLeaf(leaf: string): string {
  const trimmed = leaf.trim();
  if (!trimmed) return "Name is required (use @ for the zone apex)";
  if (trimmed === "@") return "";
  if (!LEAF_LABEL_RE.test(trimmed)) {
    return "Use letters, digits, hyphens, and dots only";
  }
  return "";
}

function validateContent(type: DNSRecordType, content: string): string {
  if (!content) return "Content is required";
  switch (type) {
    case "A":
      if (!IPV4_RE.test(content)) return "Enter a valid IPv4 address";
      return "";
    case "AAAA":
      if (!cidr.isValidAddress(content) || !content.includes(":")) {
        return "Enter a valid IPv6 address";
      }
      return "";
    case "CNAME":
      if (!FQDN_RE.test(content)) return "Enter a valid FQDN";
      return "";
  }
  return "";
}

function validateTTL(ttl: string): string {
  if (ttl === "") return "";
  const n = Number(ttl);
  if (!Number.isFinite(n) || n < 1) return "TTL must be ≥ 1";
  return "";
}

type Props = {
  zone: DNSZone;
};

export default function DNSRecordsSection({ zone }: Props) {
  const { permission } = usePermissions();
  const { confirm } = useDialog();
  const { mutate } = useSWRConfig();
  const recordsApi = useApiCall<DNSRecord>(
    `/dns/zones/${zone.id}/records`,
    true,
  );

  const sorted = useMemo(() => {
    const records = zone.records ?? [];
    return [...records].sort((a, b) => {
      if (a.name === b.name) return a.type.localeCompare(b.type);
      return a.name.localeCompare(b.name);
    });
  }, [zone.records]);

  const [editingId, setEditingId] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);

  const refreshZone = async () => {
    await mutate(`/dns/zones/${zone.id}`);
    await mutate("/dns/zones");
  };

  const remove = async (rec: DNSRecord) => {
    const choice = await confirm({
      title: `Delete record ${rec.name}?`,
      description:
        "Are you sure you want to remove this record? Peers will lose this entry on the next sync.",
      confirmText: "Delete",
      cancelText: "Cancel",
      type: "danger",
    });
    if (!choice) return;
    notify({
      title: `Record ${rec.name}`,
      description: "The record was successfully removed.",
      loadingMessage: "Deleting the record...",
      promise: recordsApi.del("", `/${rec.id}`).then(refreshZone),
    });
  };

  return (
    <div className="flex flex-col">
      <div className="flex items-center justify-between gap-3 px-[18px] py-3.5 border-b border-oz2-border-soft">
        <div>
          <h2 className="text-[14px] font-semibold text-oz2-text">
            DNS Records
          </h2>
          <p className="mt-0.5 text-[12px] text-oz2-text-muted">
            A / AAAA / CNAME records under{" "}
            <code className="font-mono text-oz2-text-2">{zone.domain}</code>.
            Use <code className="font-mono text-oz2-text-2">@</code> as the
            name for the zone apex.
          </p>
        </div>
        <OzButton
          variant="primary"
          type="button"
          disabled={
            !permission.dns_zones.create || adding || editingId !== null
          }
          onClick={() => setAdding(true)}
        >
          <PlusCircle size={14} />
          Add Record
        </OzButton>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full table-fixed text-[13px]">
          <colgroup>
            <col className="w-[80px]" />
            <col className="w-[30%]" />
            <col />
            <col className="w-[100px]" />
            <col className="w-[100px]" />
          </colgroup>
          <thead>
            <tr className="border-b border-oz2-border-soft bg-oz2-bg-sunken">
              <ThCell>Type</ThCell>
              <ThCell>Name</ThCell>
              <ThCell>Content</ThCell>
              <ThCell>TTL</ThCell>
              <ThCell className="text-right pr-4">Actions</ThCell>
            </tr>
          </thead>
          <tbody>
            {adding && (
              <RecordEditorRow
                zone={zone}
                onCancel={() => setAdding(false)}
                onSubmit={async (body) => {
                  await notify({
                    title: `Record ${body.name}`,
                    description: "Record created.",
                    loadingMessage: "Creating record...",
                    promise: recordsApi.post(body).then(refreshZone),
                  });
                  setAdding(false);
                }}
              />
            )}

            {sorted.map((rec) =>
              editingId === rec.id ? (
                <RecordEditorRow
                  key={rec.id}
                  zone={zone}
                  initial={rec}
                  onCancel={() => setEditingId(null)}
                  onSubmit={async (body) => {
                    await notify({
                      title: `Record ${body.name}`,
                      description: "Record updated.",
                      loadingMessage: "Updating record...",
                      promise: recordsApi
                        .put(body, `/${rec.id}`)
                        .then(refreshZone),
                    });
                    setEditingId(null);
                  }}
                />
              ) : (
                <RecordRow
                  key={rec.id}
                  zone={zone}
                  rec={rec}
                  canEdit={
                    permission.dns_zones.update && !adding && !editingId
                  }
                  canDelete={permission.dns_zones.delete && !adding && !editingId}
                  onEdit={() => setEditingId(rec.id)}
                  onDelete={() => remove(rec)}
                />
              ),
            )}

            {sorted.length === 0 && !adding && (
              <tr>
                <td colSpan={5} className="px-[18px] py-10">
                  <div className="flex items-start gap-3 rounded-oz2-input border border-oz2-warn/40 bg-oz2-warn-bg/40 px-3 py-2.5 text-[12.5px] text-oz2-warn">
                    <AlertTriangle size={14} className="mt-[2px] shrink-0" />
                    <p>
                      <span className="font-medium">No records yet.</span>{" "}
                      Peers in distribution groups won&apos;t receive this
                      zone until you add at least one record.
                    </p>
                  </div>
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function ThCell({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <th
      scope="col"
      className={
        "px-4 py-2.5 text-left font-mono text-[11px] font-semibold uppercase tracking-widest text-oz2-text-muted " +
        (className ?? "")
      }
    >
      {children}
    </th>
  );
}

function RecordRow({
  zone,
  rec,
  canEdit,
  canDelete,
  onEdit,
  onDelete,
}: {
  zone: DNSZone;
  rec: DNSRecord;
  canEdit: boolean;
  canDelete: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const leaf = leafFromFqdn(rec.name, zone.domain);
  return (
    <tr className="border-b border-oz2-border-soft last:border-b-0">
      <td className="px-4 py-2.5">
        <Badge variant={"gray"} className={"font-mono text-[11px]"}>
          {rec.type}
        </Badge>
      </td>
      <td className="px-4 py-2.5 font-mono text-[12.5px] text-oz2-text">
        {leaf === "@" ? (
          <span className="text-oz2-acc-text">@</span>
        ) : (
          <>
            <span>{leaf}</span>
            <span className="text-oz2-text-faint">.{zone.domain}</span>
          </>
        )}
      </td>
      <td className="px-4 py-2.5 font-mono text-[12.5px] text-oz2-text-2 truncate">
        {rec.content}
      </td>
      <td className="px-4 py-2.5 font-mono text-[12px] text-oz2-text-faint">
        {rec.ttl ?? 300}
      </td>
      <td className="px-4 py-2.5">
        <div className="flex justify-end gap-1.5">
          <button
            type="button"
            aria-label="Edit record"
            disabled={!canEdit}
            onClick={onEdit}
            className="grid h-7 w-7 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:bg-oz2-hover hover:border-oz2-border-strong disabled:cursor-not-allowed disabled:opacity-50"
          >
            <PencilLine size={13} />
          </button>
          <button
            type="button"
            aria-label="Delete record"
            disabled={!canDelete}
            onClick={onDelete}
            className="grid h-7 w-7 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:bg-oz2-hover hover:border-oz2-border-strong disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Trash2 size={13} />
          </button>
        </div>
      </td>
    </tr>
  );
}

function RecordEditorRow({
  zone,
  initial,
  onSubmit,
  onCancel,
}: {
  zone: DNSZone;
  initial?: DNSRecord;
  onSubmit: (body: DNSRecordRequest) => Promise<void>;
  onCancel: () => void;
}) {
  const [type, setType] = useState<DNSRecordType>(initial?.type ?? "A");
  const [leaf, setLeaf] = useState<string>(
    initial ? leafFromFqdn(initial.name, zone.domain) : "",
  );
  const [content, setContent] = useState(initial?.content ?? "");
  const [ttl, setTtl] = useState<string>(
    initial?.ttl !== undefined ? String(initial.ttl) : "",
  );

  const fullName = useMemo(() => fqdnFromLeaf(leaf, zone.domain), [
    leaf,
    zone.domain,
  ]);

  // Same-name conflict check for the CNAME ↔ A/AAAA mutex hint.
  // Compare against the FQDN (leaf + suffix) because that's what
  // the backend stores.
  const conflict = useMemo(() => {
    const same = (zone.records ?? []).filter(
      (r) => r.id !== initial?.id && r.name === fullName,
    );
    if (same.length === 0) return "";
    if (type === "CNAME" && same.some((r) => r.type !== "CNAME")) {
      return "An A/AAAA record already exists for this name — CNAME is not allowed.";
    }
    if (type !== "CNAME" && same.some((r) => r.type === "CNAME")) {
      return "A CNAME already exists for this name — A/AAAA is not allowed.";
    }
    return "";
  }, [zone.records, fullName, type, initial?.id]);

  const leafError = useMemo(() => validateLeaf(leaf), [leaf]);
  const contentError = useMemo(
    () => (content ? validateContent(type, content) : ""),
    [type, content],
  );
  const ttlError = useMemo(() => validateTTL(ttl), [ttl]);

  const canSubmit =
    !leafError && !contentError && !ttlError && !conflict && !!content;

  const submit = async () => {
    const body: DNSRecordRequest = {
      name: fullName,
      type,
      content,
      ttl: ttl === "" ? undefined : Number(ttl),
    };
    await onSubmit(body);
  };

  const suffixNode = (
    <span className="ml-1 font-mono text-[11.5px] text-oz2-text-faint">
      .{zone.domain}
    </span>
  );

  return (
    <>
      <tr className="border-b border-oz2-border-soft bg-oz2-acc-soft/30">
        <td className="px-4 py-3 align-top">
          <select
            value={type}
            onChange={(e) => setType(e.target.value as DNSRecordType)}
            className="h-[34px] w-full rounded-oz2-input border border-oz2-border bg-oz2-surface px-2 font-mono text-[12.5px]"
          >
            <option value="A">A</option>
            <option value="AAAA">AAAA</option>
            <option value="CNAME">CNAME</option>
          </select>
        </td>
        <td className="px-4 py-3 align-top">
          <OzInput
            placeholder={"www  (or @ for apex)"}
            mono
            value={leaf}
            onChange={(e) => setLeaf(e.target.value)}
            error={leaf ? leafError : ""}
            suffix={leaf && leaf.trim() !== "@" ? suffixNode : undefined}
            autoFocus
          />
        </td>
        <td className="px-4 py-3 align-top">
          <OzInput
            placeholder={
              type === "CNAME"
                ? "target.example.com"
                : type === "A"
                  ? "10.0.0.5"
                  : "fd00::1"
            }
            mono
            value={content}
            onChange={(e) => setContent(e.target.value)}
            error={content ? contentError : ""}
          />
        </td>
        <td className="px-4 py-3 align-top">
          <OzInput
            placeholder={"300"}
            type={"number"}
            value={ttl}
            onChange={(e) => setTtl(e.target.value)}
            error={ttlError}
          />
        </td>
        <td className="px-4 py-3 align-top">
          <div className="flex justify-end gap-1.5">
            <button
              type="button"
              aria-label="Save record"
              disabled={!canSubmit}
              onClick={submit}
              className="grid h-[34px] w-[34px] shrink-0 place-items-center rounded-oz2-input border border-oz2-acc/40 bg-oz2-acc text-white hover:bg-oz2-acc-hover disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Check size={15} />
            </button>
            <button
              type="button"
              aria-label="Cancel"
              onClick={onCancel}
              className="grid h-[34px] w-[34px] shrink-0 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 hover:bg-oz2-hover hover:border-oz2-border-strong"
            >
              <X size={15} />
            </button>
          </div>
        </td>
      </tr>
      {conflict && (
        <tr className="border-b border-oz2-border-soft bg-oz2-acc-soft/30">
          <td colSpan={5} className="px-4 pb-3">
            <p className="text-[11.5px] text-oz2-err">{conflict}</p>
          </td>
        </tr>
      )}
    </>
  );
}
