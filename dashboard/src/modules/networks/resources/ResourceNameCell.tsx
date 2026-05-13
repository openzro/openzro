import DescriptionWithTooltip from "@components/ui/DescriptionWithTooltip";
import TextWithTooltip from "@components/ui/TextWithTooltip";
import { cn } from "@utils/helpers";
import { GlobeIcon, NetworkIcon, WorkflowIcon } from "lucide-react";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { NetworkResource } from "@/interfaces/Network";
import { useNetworksContext } from "@/modules/networks/NetworkProvider";

type Props = {
  resource: NetworkResource;
};

export default function ResourceNameCell({ resource }: Readonly<Props>) {
  const { permission } = usePermissions();
  const { network, openResourceModal } = useNetworksContext();

  const icon =
    resource.type === "domain" ? (
      <GlobeIcon size={15} />
    ) : resource.type === "subnet" ? (
      <NetworkIcon size={15} />
    ) : (
      <WorkflowIcon size={15} />
    );

  return (
    <button
      className={"flex gap-3 items-center group"}
      onClick={() => {
        if (!network || !permission.networks.update) return;
        openResourceModal(network, resource);
      }}
    >
      {/*
        Row-level resource tile. Light mode mirrors the modal exactly
        (amber-100 / amber-700). Dark mode swaps the modal's amber-500/15
        — which glowed loud when stacked across a dozen rows — for a
        deeper amber-950 base + amber-200 icon, so the tile sits flat
        against the dark surface like the OS / process pills do.
      */}
      <div
        className={cn(
          "flex h-8 w-8 shrink-0 items-center justify-center rounded-[10px] select-none",
          "bg-amber-100 text-amber-700",
          "dark:bg-amber-950/50 dark:text-amber-200/90",
        )}
      >
        {icon}
      </div>
      <div
        className={cn(
          "flex flex-col gap-0 text-left",
          "text-oz2-text",
        )}
      >
        <TextWithTooltip
          text={resource.name}
          maxChars={25}
          className={"font-medium"}
        />
        <DescriptionWithTooltip
          maxChars={25}
          className={"text-oz2-text-muted mt-0.5"}
          text={resource.description}
        />
      </div>
    </button>
  );
}
