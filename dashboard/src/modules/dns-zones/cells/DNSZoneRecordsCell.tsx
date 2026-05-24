import Badge from "@components/Badge";
import { FileText } from "lucide-react";
import React from "react";
import { DNSZone } from "@/interfaces/DNSZone";

type Props = {
  zone: DNSZone;
};

export default function DNSZoneRecordsCell({ zone }: Props) {
  const count = zone.records?.length ?? 0;
  if (count === 0) {
    return (
      <Badge variant={"gray"} className={"font-mono"}>
        <FileText size={10} className={"mr-1"} />
        Empty
      </Badge>
    );
  }
  return (
    <Badge variant={"gray"} className={"font-mono"}>
      <FileText size={10} className={"mr-1"} />
      {count} record{count === 1 ? "" : "s"}
    </Badge>
  );
}
