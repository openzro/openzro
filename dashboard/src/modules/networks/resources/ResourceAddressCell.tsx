import CopyToClipboardText from "@components/CopyToClipboardText";
import React from "react";
import { NetworkResource } from "@/interfaces/Network";

type Props = {
  resource: NetworkResource;
};
export default function ResourceAddressCell({ resource }: Readonly<Props>) {
  // For type=domain, the management server aggregates the IPs each
  // peer agent resolved this domain to (last 24h of flow events,
  // returned as resolved_addresses). Surface them under the domain
  // string so operators can see what the resource currently points
  // to without having to wait for traffic in this peer's view —
  // especially useful in split-horizon setups where different peers
  // see different IPs. Absent / empty on host & subnet (their
  // address field already carries the explicit IP / prefix).
  const isDomain = resource.type === "domain";
  const resolvedIPs = isDomain ? resource.resolved_addresses ?? [] : [];
  return (
    <div className={"flex flex-col gap-0.5"}>
      <CopyToClipboardText
        message={`${resource.address} has been copied to your clipboard`}
      >
        <div
          className={
            "font-mono dark:text-nb-gray-300 pt-1 flex gap-2 items-center text-[.82rem]"
          }
        >
          {resource.address}
        </div>
      </CopyToClipboardText>
      {resolvedIPs.length > 0 && (
        <div
          className={
            "font-mono text-[.7rem] leading-tight text-nb-gray-500 dark:text-nb-gray-400 max-w-[20rem] truncate"
          }
          title={`Currently resolves to (observed in the last 24h):\n${resolvedIPs.join(", ")}`}
        >
          → {resolvedIPs.join(", ")}
        </div>
      )}
    </div>
  );
}
