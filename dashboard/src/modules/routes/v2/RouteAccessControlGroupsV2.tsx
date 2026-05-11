"use client";

import * as React from "react";
import OzMultipleGroups from "@/components/v2/OzMultipleGroups";
import { useGroups } from "@/contexts/GroupsProvider";
import { Group } from "@/interfaces/Group";
import { Route } from "@/interfaces/Route";
import EmptyRow from "@/modules/common-table-rows/EmptyRow";

type Props = {
  route: Route;
};

export default function RouteAccessControlGroupsV2({ route }: Props) {
  const { groups } = useGroups();
  if (!route?.access_control_groups) return <EmptyRow />;

  const allGroups = route.access_control_groups
    .map((id) => groups?.find((g) => g.id == id))
    .filter((g): g is Group => g !== undefined);

  return <OzMultipleGroups groups={allGroups} />;
}
