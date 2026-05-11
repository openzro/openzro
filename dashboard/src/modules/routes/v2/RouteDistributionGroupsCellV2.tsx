"use client";

import * as React from "react";
import OzMultipleGroups from "@/components/v2/OzMultipleGroups";
import { useGroups } from "@/contexts/GroupsProvider";
import { Group } from "@/interfaces/Group";
import { Route } from "@/interfaces/Route";

type Props = {
  route: Route;
};

export default function RouteDistributionGroupsCellV2({ route }: Props) {
  const { groups } = useGroups();

  const allGroups = route.groups
    .map((id) => groups?.find((g) => g.id == id))
    .filter((g): g is Group => g !== undefined);

  return <OzMultipleGroups groups={allGroups} />;
}
