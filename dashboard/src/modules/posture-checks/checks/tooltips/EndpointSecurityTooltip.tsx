import FullTooltip from "@components/FullTooltip";
import useFetchApi from "@utils/api";
import * as React from "react";
import { MDMProvider } from "@/interfaces/MDMProvider";
import { EndpointSecurityCheck } from "@/interfaces/PostureCheck";

type Props = {
  check?: EndpointSecurityCheck;
  children?: React.ReactNode;
};

export const EndpointSecurityTooltip = ({ check, children }: Props) => {
  // Mount-guarded by the call site (cell wraps the tooltip only when
  // endpoint_security_check is set), so this fetch only runs on pages
  // that actually need the provider name. SWR dedupes by URL key, so
  // multiple rows with endpoint security on the same page share one
  // network round-trip.
  const { data: providers } = useFetchApi<MDMProvider[]>(
    "/admin/mdm-providers",
  );

  if (!check) return <>{children}</>;

  const provider = providers?.find((p) => p.id === check.provider_id);
  const providerName = provider ? provider.name : `Provider #${check.provider_id}`;

  return (
    <FullTooltip
      className={"w-full min-w-0"}
      interactive={true}
      contentClassName={"p-3"}
      content={
        <div className={"text-neutral-300 text-sm max-w-xs flex flex-col gap-1"}>
          <div>
            <span className={"text-emerald-400 font-semibold"}>Require</span>{" "}
            device compliance from{" "}
            <span className={"font-medium text-white"}>{providerName}</span>
          </div>
          <div className={"text-xs text-neutral-400"}>
            On lookup failure:{" "}
            <span className={"font-medium"}>
              {check.fail_open ? "allow (fail-open)" : "deny (fail-closed)"}
            </span>
          </div>
        </div>
      }
    >
      {children}
    </FullTooltip>
  );
};
