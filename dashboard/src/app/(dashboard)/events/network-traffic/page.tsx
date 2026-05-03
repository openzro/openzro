"use client";

import Breadcrumbs from "@components/Breadcrumbs";
import InlineLink from "@components/InlineLink";
import Paragraph from "@components/Paragraph";
import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useFetchApi from "@utils/api";
import { ExternalLinkIcon, NetworkIcon } from "lucide-react";
import React from "react";
import ActivityIcon from "@/assets/icons/ActivityIcon";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { NetworkTrafficEventsResponse } from "@/interfaces/NetworkTrafficEvent";
import PageContainer from "@/layouts/PageContainer";
import NetworkTrafficTimeline from "@/modules/network-traffic/NetworkTrafficTimeline";

export default function NetworkTraffic() {
  const { permission } = usePermissions();

  // The endpoint is GET /api/network-traffic-events. Server responds
  // with an empty list when the hot store is not configured
  // (engine=none) — we surface the same empty-state UI either way,
  // so the page never hangs on a config mismatch.
  const { data, isLoading } =
    useFetchApi<NetworkTrafficEventsResponse>("/network-traffic-events");

  return (
    <PageContainer>
      <div className={"p-default py-6"}>
        <Breadcrumbs>
          <Breadcrumbs.Item
            label={"Activity"}
            disabled={true}
            icon={<ActivityIcon size={13} />}
          />
          <Breadcrumbs.Item
            href={"/events/network-traffic"}
            label={"Network Traffic"}
            icon={<NetworkIcon size={18} />}
          />
        </Breadcrumbs>
        <h1>Network Traffic Events</h1>
        <Paragraph>
          Per-flow records reported by your peers — connection starts,
          ends, and drops. Useful for forensics, capacity planning, and
          validating that your access policies match traffic in the
          wild.
        </Paragraph>
        <Paragraph>
          Older events live in your configured streaming target (SIEM)
          or cold archive. Read more about{" "}
          <InlineLink
            href={"https://docs.openzro.io/how-to/network-traffic-events"}
            target={"_blank"}
          >
            Network Traffic Events
            <ExternalLinkIcon size={12} />
          </InlineLink>
          .
        </Paragraph>
      </div>
      <RestrictedAccess
        page={"Network Traffic"}
        hasAccess={permission.events.read}
      >
        <NetworkTrafficTimeline events={data?.events} isLoading={isLoading} />
      </RestrictedAccess>
    </PageContainer>
  );
}
