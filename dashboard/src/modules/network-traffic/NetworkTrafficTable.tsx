"use client";

import { DataTable } from "@components/table/DataTable";
import DataTableHeader from "@components/table/DataTableHeader";
import DataTableRefreshButton from "@components/table/DataTableRefreshButton";
import { DataTableRowsPerPage } from "@components/table/DataTableRowsPerPage";
import { ColumnDef } from "@tanstack/react-table";
import dayjs from "dayjs";
import { ArrowDown, ArrowUp, Ban, Play, Square } from "lucide-react";
import React from "react";
import { NetworkTrafficEvent } from "@/interfaces/NetworkTrafficEvent";

type Props = {
  events?: NetworkTrafficEvent[];
  isLoading: boolean;
  headingTarget?: HTMLHeadingElement | null;
};

// Columns are intentionally minimal — the API exposes 8 filters but
// the dashboard MVP only renders the most-asked columns. Filters and
// the full column set arrive in a follow-up.
const columns: ColumnDef<NetworkTrafficEvent>[] = [
  {
    accessorKey: "received_at",
    header: ({ column }) => (
      <DataTableHeader column={column}>Time</DataTableHeader>
    ),
    sortingFn: "datetime",
    cell: ({ row }) => (
      <span className="font-mono text-xs text-nb-gray-300">
        {dayjs(row.original.received_at).format("YYYY-MM-DD HH:mm:ss")}
      </span>
    ),
  },
  {
    accessorKey: "type",
    header: ({ column }) => (
      <DataTableHeader column={column}>Type</DataTableHeader>
    ),
    cell: ({ row }) => <TypeBadge type={row.original.type} />,
  },
  {
    accessorKey: "direction",
    header: ({ column }) => (
      <DataTableHeader column={column}>Direction</DataTableHeader>
    ),
    cell: ({ row }) => <DirectionBadge direction={row.original.direction} />,
  },
  {
    accessorKey: "peer_id",
    header: ({ column }) => (
      <DataTableHeader column={column}>Peer</DataTableHeader>
    ),
    cell: ({ row }) => (
      <span className="font-mono text-xs">{row.original.peer_id}</span>
    ),
  },
  {
    id: "src_dst",
    header: ({ column }) => (
      <DataTableHeader column={column}>Source → Destination</DataTableHeader>
    ),
    cell: ({ row }) => {
      const e = row.original;
      const src = formatEndpoint(e.source_ip, e.source_port);
      const dst = formatEndpoint(e.dest_ip, e.dest_port);
      return (
        <span className="font-mono text-xs">
          {src} <span className="text-nb-gray-400">→</span> {dst}
        </span>
      );
    },
  },
  {
    accessorKey: "protocol",
    header: ({ column }) => (
      <DataTableHeader column={column}>Protocol</DataTableHeader>
    ),
    cell: ({ row }) => (
      <span className="text-xs uppercase text-nb-gray-200">
        {protocolName(row.original.protocol)}
      </span>
    ),
  },
  {
    id: "bytes",
    header: ({ column }) => (
      <DataTableHeader column={column}>Bytes (rx / tx)</DataTableHeader>
    ),
    accessorFn: (e) => e.rx_bytes + e.tx_bytes,
    cell: ({ row }) => (
      <span className="font-mono text-xs">
        {formatBytes(row.original.rx_bytes)} /{" "}
        {formatBytes(row.original.tx_bytes)}
      </span>
    ),
  },
];

export default function NetworkTrafficTable({
  events,
  isLoading,
  headingTarget,
}: Props) {
  return (
    <DataTable
      isLoading={isLoading}
      text={"network traffic events"}
      columns={columns}
      data={events ?? []}
      searchPlaceholder={"Search by peer or IP…"}
      headingTarget={headingTarget}
      paginationPaddingClassName={"px-default"}
      rightSide={(table) => (
        <>
          <DataTableRefreshButton
            isDisabled={isLoading}
            onClick={() => location.reload()}
          />
          <DataTableRowsPerPage table={table} />
        </>
      )}
    />
  );
}

function TypeBadge({ type }: { type: NetworkTrafficEvent["type"] }) {
  const map: Record<string, { icon: React.ReactNode; cls: string }> = {
    start: { icon: <Play size={12} />, cls: "text-emerald-400" },
    end: { icon: <Square size={12} />, cls: "text-nb-gray-200" },
    drop: { icon: <Ban size={12} />, cls: "text-red-400" },
    unknown: { icon: null, cls: "text-nb-gray-400" },
  };
  const m = map[type] ?? map.unknown;
  return (
    <span className={`inline-flex items-center gap-1 text-xs ${m.cls}`}>
      {m.icon} {type}
    </span>
  );
}

function DirectionBadge({
  direction,
}: {
  direction: NetworkTrafficEvent["direction"];
}) {
  if (direction === "ingress") {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-sky-400">
        <ArrowDown size={12} /> in
      </span>
    );
  }
  if (direction === "egress") {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-violet-400">
        <ArrowUp size={12} /> out
      </span>
    );
  }
  return <span className="text-xs text-nb-gray-400">—</span>;
}

function formatEndpoint(ip: string, port?: number): string {
  if (port) {
    return `${ip}:${port}`;
  }
  return ip;
}

// protocolName resolves IANA-assigned protocol numbers to a friendly
// label for the common cases. Unknown numbers render as "p<n>" so
// dashboards always show something readable.
function protocolName(protocol: number): string {
  switch (protocol) {
    case 1:
      return "icmp";
    case 6:
      return "tcp";
    case 17:
      return "udp";
    case 58:
      return "icmpv6";
    case 132:
      return "sctp";
    default:
      return `p${protocol}`;
  }
}

function formatBytes(b: number): string {
  if (b < 1024) return `${b} B`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`;
  if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`;
  return `${(b / 1024 / 1024 / 1024).toFixed(2)} GB`;
}
