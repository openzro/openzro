"use client";

import { DataTable } from "@components/table/DataTable";
import DataTableHeader from "@components/table/DataTableHeader";
import DataTableRefreshButton from "@components/table/DataTableRefreshButton";
import { DataTableRowsPerPage } from "@components/table/DataTableRowsPerPage";
import { ColumnDef } from "@tanstack/react-table";
import dayjs from "dayjs";
import { isEmpty } from "lodash";
import { ArrowDown, ArrowUp, Ban, GlobeIcon, Play, Square } from "lucide-react";
import React, { useMemo } from "react";
import RoundedFlag from "@/assets/countries/RoundedFlag";
import { usePeers } from "@/contexts/PeersProvider";
import { NetworkTrafficEvent } from "@/interfaces/NetworkTrafficEvent";
import { Peer } from "@/interfaces/Peer";

type Props = {
  events?: NetworkTrafficEvent[];
  isLoading: boolean;
  headingTarget?: HTMLHeadingElement | null;
};

// Enriched row carries the resolved Peer objects so cell renderers
// don't repeat the ID/IP lookup. The lookup itself happens once
// per render in the parent component (peers list is cached at the
// PeersProvider level so the cost is two Map probes per row).
type EnrichedEvent = NetworkTrafficEvent & {
  reportingPeer?: Peer;
  sourcePeer?: Peer;
  destPeer?: Peer;
};

const columns: ColumnDef<EnrichedEvent>[] = [
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
    id: "reporting_peer",
    header: ({ column }) => (
      <DataTableHeader column={column}>Reporting peer</DataTableHeader>
    ),
    cell: ({ row }) => (
      <PeerCell peer={row.original.reportingPeer} fallbackId={row.original.peer_id} />
    ),
  },
  {
    id: "source",
    header: ({ column }) => (
      <DataTableHeader column={column}>Source</DataTableHeader>
    ),
    cell: ({ row }) => (
      <EndpointCell
        peer={row.original.sourcePeer}
        ip={row.original.source_ip}
        port={row.original.source_port}
      />
    ),
  },
  {
    id: "destination",
    header: ({ column }) => (
      <DataTableHeader column={column}>Destination</DataTableHeader>
    ),
    cell: ({ row }) => (
      <EndpointCell
        peer={row.original.destPeer}
        ip={row.original.dest_ip}
        port={row.original.dest_port}
      />
    ),
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
  const { peers } = usePeers();

  // Two parallel indexes so cell renderers resolve the reporting peer
  // (by gRPC peer ID) AND the source/dest peers (by mesh IP) in O(1).
  // The mesh IP index trades a slightly larger footprint for the
  // human-readable Source / Destination columns operators expect.
  const enriched: EnrichedEvent[] = useMemo(() => {
    const byID = new Map<string, Peer>();
    const byIP = new Map<string, Peer>();
    if (peers) {
      for (const p of peers) {
        if (p.id) byID.set(p.id, p);
        if (p.ip) byIP.set(p.ip, p);
      }
    }
    return (events ?? []).map((e) => ({
      ...e,
      reportingPeer: byID.get(e.peer_id),
      sourcePeer: byIP.get(e.source_ip),
      destPeer: byIP.get(e.dest_ip),
    }));
  }, [events, peers]);

  return (
    <DataTable
      isLoading={isLoading}
      text={"network traffic events"}
      columns={columns}
      data={enriched}
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

// PeerCell renders the resolved Peer with name + flag, or falls back
// to the truncated peer_id when the peer was deleted between the
// event being captured and this view loading. Hostname is shown
// underneath in muted mono so operators can disambiguate two peers
// that share a friendly name.
function PeerCell({
  peer,
  fallbackId,
}: {
  peer?: Peer;
  fallbackId: string;
}) {
  if (!peer) {
    return (
      <span className="font-mono text-xs text-nb-gray-400" title={fallbackId}>
        {truncateID(fallbackId)}
      </span>
    );
  }
  return (
    <div className="flex items-center gap-2">
      {!isEmpty(peer.country_code) ? (
        <RoundedFlag country={peer.country_code} size={18} />
      ) : (
        <GlobeIcon size={14} className="text-nb-gray-400" />
      )}
      <div className="flex flex-col">
        <span className="text-xs text-white">{peer.name}</span>
        {peer.hostname && peer.hostname !== peer.name && (
          <span className="font-mono text-[10px] text-nb-gray-400">
            {peer.hostname}
          </span>
        )}
      </div>
    </div>
  );
}

// EndpointCell renders the IP:port pair, with a peer name ribbon
// when the IP belongs to a known mesh peer. Off-mesh IPs (egress
// to the internet, scanners, etc.) just show the address — that's
// useful audit signal on its own.
function EndpointCell({
  peer,
  ip,
  port,
}: {
  peer?: Peer;
  ip: string;
  port?: number;
}) {
  return (
    <div className="flex flex-col">
      {peer ? (
        <span className="text-xs text-white">{peer.name}</span>
      ) : null}
      <span className="font-mono text-[11px] text-nb-gray-300">
        {port ? `${ip}:${port}` : ip}
      </span>
    </div>
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

// truncateID keeps the leading prefix of a peer ID so operators can
// recognise it from `openzro status` output without flooding the
// column. The full ID stays in the title attribute for hover.
function truncateID(id: string): string {
  if (id.length <= 12) return id;
  return id.slice(0, 8) + "…";
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
