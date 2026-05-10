"use client";

import Breadcrumbs from "@components/Breadcrumbs";
import InlineLink from "@components/InlineLink";
import Paragraph from "@components/Paragraph";
import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import { ExternalLinkIcon, NetworkIcon } from "lucide-react";
import React from "react";
import ActivityIcon from "@/assets/icons/ActivityIcon";
import PeersProvider from "@/contexts/PeersProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import PageContainer from "@/layouts/PageContainer";
import EventsTabs from "@/modules/events/v2/EventsTabs";
import NetworkTrafficTimeline from "@/modules/network-traffic/NetworkTrafficTimeline";

export default function NetworkTraffic() {
  const { permission } = usePermissions();

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
          Streaming exporters (Datadog / Elastic / HTTP) ship every
          event to your SIEM in real time for alerting. Configured
          cold archives (S3 / GCS Parquet) back this view past the
          hot retention window — older queries are served from the
          archive automatically. Read more about{" "}
          <InlineLink
            href={"https://docs.openzro.io/how-to/network-traffic-events"}
            target={"_blank"}
          >
            Network Traffic Events
            <ExternalLinkIcon size={12} />
          </InlineLink>
          .
        </Paragraph>
        {/* Transitional EventsTabs — this page still ships legacy
            paint inside the v2 chrome until phase 5.12 lands the
            v2 body. Mounting EventsTabs here gives the operator a
            way back to /events/audit (which is already on the v2
            timeline). Drop it once the body is ported and the page
            wraps EventsTabs natively like AuditTimelineV2 does. */}
        <div className={"mt-4"}>
          <EventsTabs />
        </div>
      </div>
      <RestrictedAccess
        page={"Network Traffic"}
        hasAccess={permission.events.read}
      >
        {/* PeersProvider is required so the timeline can resolve
            source_ip / dest_ip / peer_id back to friendly peer names
            and country flags. Without it, every row falls through to
            the bare IP fallback. */}
        <PeersProvider>
          <NetworkTrafficTimeline />
        </PeersProvider>
      </RestrictedAccess>
    </PageContainer>
  );
}
