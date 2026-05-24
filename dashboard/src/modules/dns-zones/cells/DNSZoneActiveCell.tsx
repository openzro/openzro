"use client";

import { notify } from "@components/Notification";
import { ToggleSwitch } from "@components/ToggleSwitch";
import { useApiCall } from "@utils/api";
import React from "react";
import { useSWRConfig } from "swr";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { DNSZone, DNSZoneRequest } from "@/interfaces/DNSZone";

type Props = {
  zone: DNSZone;
};

export default function DNSZoneActiveCell({ zone }: Readonly<Props>) {
  const zoneRequest = useApiCall<DNSZone>("/dns/zones");
  const { mutate } = useSWRConfig();
  const { permission } = usePermissions();

  const checked = zone.enabled ?? true;

  const update = async (next: boolean) => {
    const body: DNSZoneRequest = {
      name: zone.name,
      domain: zone.domain,
      enabled: next,
      enable_search_domain: zone.enable_search_domain,
      distribution_groups: zone.distribution_groups,
    };
    notify({
      title: zone.name,
      description:
        "Zone was successfully" + (next ? " enabled" : " disabled") + ".",
      loadingMessage: "Updating your zone...",
      promise: zoneRequest.put(body, `/${zone.id}`).then(() => {
        mutate("/dns/zones");
      }),
    });
  };

  return (
    <div className={"flex"}>
      <ToggleSwitch
        disabled={!permission.dns_zones.update}
        checked={checked}
        size={"small"}
        onClick={() => update(!checked)}
      />
    </div>
  );
}
