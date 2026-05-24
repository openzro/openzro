"use client";

import FullScreenLoading from "@components/ui/FullScreenLoading";
import { PageNotFound } from "@components/ui/PageNotFound";
import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useRedirect from "@hooks/useRedirect";
import useFetchApi from "@utils/api";
import { useSearchParams } from "next/navigation";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { DNSZone } from "@/interfaces/DNSZone";
import DNSZoneDetailPage from "@/modules/dns-zones/DNSZoneDetailPage";

export default function DNSZonePage() {
  const params = useSearchParams();
  const zoneId = params.get("id");
  const { permission, isRestricted } = usePermissions();

  // Guard the fetch against the no-id case — useRedirect bounces us
  // back to /dns/zones below, but until that runs we don't want to
  // hit `/dns/zones` and end up with the wrong-shape response in the
  // SWR cache.
  const {
    data: zone,
    isLoading,
    error,
  } = useFetchApi<DNSZone>(
    zoneId ? `/dns/zones/${zoneId}` : "",
    false,
    true,
    !!zoneId,
  );

  useRedirect("/dns/zones", false, !zoneId || isRestricted);

  if (!permission.dns_zones.read) {
    return (
      <div className="space-y-6 p-8">
        <RestrictedAccess page={"DNS Zone"} />
      </div>
    );
  }

  if (error) {
    return (
      <PageNotFound
        title={error?.message}
        description={
          "The zone you are attempting to access cannot be found. It may have been deleted, or you may not have permission to view it. Please verify the URL or return to the list."
        }
      />
    );
  }

  return zone && !isLoading ? (
    <DNSZoneDetailPage zone={zone} />
  ) : (
    <FullScreenLoading />
  );
}
