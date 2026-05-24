import MultipleGroups from "@components/ui/MultipleGroups";
import * as React from "react";
import { useGroups } from "@/contexts/GroupsProvider";
import { DNSZone } from "@/interfaces/DNSZone";
import { Group } from "@/interfaces/Group";

type Props = {
  zone: DNSZone;
};

export default function DNSZoneDistributionCell({ zone }: Props) {
  const { groups } = useGroups();

  const allGroups = zone.distribution_groups
    .map((id) => groups?.find((g) => g.id == id))
    .filter((g) => g != undefined) as Group[];

  return <MultipleGroups groups={allGroups} />;
}
