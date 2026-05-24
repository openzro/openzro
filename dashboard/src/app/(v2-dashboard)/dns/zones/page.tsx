"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useFetchApi from "@utils/api";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { DNSZone } from "@/interfaces/DNSZone";
import DNSZonesTable from "@/modules/dns-zones/DNSZonesTable";

export default function DNSZones() {
  const { permission } = usePermissions();
  const { data: zones, isLoading } = useFetchApi<DNSZone[]>("/dns/zones");

  return (
    <RestrictedAccess hasAccess={permission.dns_zones.read}>
      <DNSZonesTable zones={zones} isLoading={isLoading} />
    </RestrictedAccess>
  );
}
