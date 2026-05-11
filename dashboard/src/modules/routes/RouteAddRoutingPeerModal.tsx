"use client";

import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { NetworkRouteSelector } from "@components/NetworkRouteSelector";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { PeerSelector } from "@components/PeerSelector";
import Separator from "@components/Separator";
import { uniqBy } from "lodash";
import { ExternalLinkIcon, PlusCircle } from "lucide-react";
import React, { useMemo, useState } from "react";
import NetworkRoutesIcon from "@/assets/icons/NetworkRoutesIcon";
import OzButton from "@/components/v2/OzButton";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import { useRoutes } from "@/contexts/RoutesProvider";
import { Peer } from "@/interfaces/Peer";
import { GroupedRoute, Route } from "@/interfaces/Route";
import useGroupHelper from "@/modules/groups/useGroupHelper";

type Props = {
  groupedRoute?: GroupedRoute;
  modal: boolean;
  setModal: (modal: boolean) => void;
  peer?: Peer;
};

export default function RouteAddRoutingPeerModal({
  groupedRoute,
  modal,
  setModal,
  peer,
}: Props) {
  return (
    <Modal open={modal} onOpenChange={setModal}>
      {modal && (
        <Content
          onSuccess={() => setModal(false)}
          groupedRoute={groupedRoute}
          peer={peer}
        />
      )}
    </Modal>
  );
}

type ModalProps = {
  onSuccess?: (route: Route) => void;
  groupedRoute?: GroupedRoute;
  peer?: Peer;
};

function Content({ onSuccess, groupedRoute, peer }: ModalProps) {
  const { createRoute } = useRoutes();

  const [routingPeer, setRoutingPeer] = useState<Peer | undefined>(
    peer || undefined,
  );
  const [groups, setGroups, { getGroupsToUpdate }] = useGroupHelper({
    initial: [],
  });

  /**
   * Access Control Groups
   */
  const [
    accessControlGroups,
    setAccessControlGroups,
    { getGroupsToUpdate: getAccessControlGroupsToUpdate },
  ] = useGroupHelper({
    initial: [],
  });

  const [routeNetwork, setRouteNetwork] = useState<GroupedRoute | undefined>(
    groupedRoute,
  );

  const excludedPeers = useMemo(() => {
    if (!routeNetwork) return [];
    if (!routeNetwork.routes) return [];
    return routeNetwork.routes
      .map((r) => {
        if (!r.peer) return undefined;
        return r.peer;
      })
      .filter((p) => p != undefined) as string[];
  }, [routeNetwork]);

  // Add peer to route
  const createRouteHandler = async () => {
    if (!routeNetwork) return;
    // Create groups that do not exist
    const g2 = getGroupsToUpdate();
    const g3 = getAccessControlGroupsToUpdate();
    const createOrUpdateGroups = uniqBy([...g2, ...g3], "name").map(
      (g) => g.promise,
    );
    const createdGroups = await Promise.all(
      createOrUpdateGroups.map((call) => call()),
    );
    // Get distribution group ids
    const groupIds = groups
      .map((g) => {
        const find = createdGroups.find((group) => group.name === g.name);
        return find?.id;
      })
      .filter((g) => g !== undefined) as string[];

    let accessControlGroupIds: string[] | undefined = undefined;
    if (accessControlGroups.length > 0) {
      accessControlGroupIds = accessControlGroups
        .map((g) => {
          const find = createdGroups.find((group) => group.name === g.name);
          return find?.id;
        })
        .filter((g) => g !== undefined) as string[];
    }

    let useRange = false;
    if (routeNetwork?.domains) {
      useRange = routeNetwork.domains.length <= 0;
    } else {
      useRange = true;
    }

    createRoute(
      {
        network_id: routeNetwork.network_id,
        description: "",
        enabled: true,
        peer: routingPeer?.id || undefined,
        peer_groups: undefined,
        network: useRange ? routeNetwork.network : undefined,
        domains: useRange ? undefined : routeNetwork.domains,
        keep_route: routeNetwork.keep_route || false,
        metric: 9999,
        masquerade: true,
        groups: groupIds,
        access_control_groups: accessControlGroupIds || undefined,
      },
      onSuccess,
      "Peer was successfully added to the route",
    );
  };

  // Is button disabled
  const isDisabled = useMemo(() => {
    return !routingPeer || groups.length == 0 || !routeNetwork;
  }, [routingPeer, groups, routeNetwork]);

  const singleRoutingPeerGroups = useMemo(() => {
    if (!routingPeer) return [];
    return routingPeer?.groups;
  }, [routingPeer]);

  return (
    <ModalContent maxWidthClass={"max-w-2xl"}>
      <ModalHeader
        icon={<NetworkRoutesIcon className={"fill-openzro"} />}
        title={"Add New Routing Peer"}
        description={
          "When you add multiple routing peers, Openzro enables high availability for this network."
        }
        color={"openzro"}
      />

      <Separator />

      <div className={"flex flex-col gap-6 px-8 py-6"}>
        <div>
          <OzLabel>Network Identifier</OzLabel>
          <OzHelpText className="mb-2">
            Network name and CIDR that you are adding the route to.
          </OzHelpText>
          <NetworkRouteSelector
            disabled={groupedRoute != undefined}
            value={routeNetwork}
            onChange={setRouteNetwork}
          />
        </div>
        <div>
          <OzLabel>Routing Peer</OzLabel>
          <OzHelpText className="mb-2">
            Assign a single peer as a routing peer for the network route.
          </OzHelpText>
          <PeerSelector
            onChange={setRoutingPeer}
            value={routingPeer}
            disabled={peer != undefined}
            excludedPeers={excludedPeers}
          />
        </div>
        <div>
          <OzLabel>Distribution Groups</OzLabel>
          <OzHelpText className="mb-2">
            Advertise this route to peers that belong to the following groups
          </OzHelpText>
          <PeerGroupSelector onChange={setGroups} values={groups} />
        </div>
        <div>
          <OzLabel optional>Access Control Groups</OzLabel>
          <OzHelpText className="mb-2">
            These groups offer a more granular control of internal services in
            your network. They can be used in access control policies to limit
            and control access of this route.
          </OzHelpText>
          <PeerGroupSelector
            onChange={setAccessControlGroups}
            values={accessControlGroups}
          />
        </div>
      </div>
      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={
                "https://docs.openzro.io/how-to/routing-traffic-to-private-networks"
              }
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Network Routes
              <ExternalLinkIcon size={12} />
            </a>
          </p>
        </div>
        <div className={"flex gap-3 w-full justify-end"}>
          <ModalClose asChild={true}>
            <OzButton variant={"default"}>Cancel</OzButton>
          </ModalClose>

          <OzButton
            variant={"primary"}
            disabled={isDisabled}
            onClick={createRouteHandler}
          >
            <PlusCircle size={16} />
            Add Route
          </OzButton>
        </div>
      </ModalFooter>
    </ModalContent>
  );
}
