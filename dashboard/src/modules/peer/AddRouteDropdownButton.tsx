"use client";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@components/DropdownMenu";
import { Modal } from "@components/modal/Modal";
import { ChevronDown, NetworkIcon, PlusCircle } from "lucide-react";
import React, { useState } from "react";
import OzButton from "@/components/v2/OzButton";
import { usePeer } from "@/contexts/PeerProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import RouteAddRoutingPeerModal from "@/modules/routes/RouteAddRoutingPeerModal";
import { RouteModalContent } from "@/modules/routes/RouteModal";

export default function AddRouteDropdownButton() {
  const [modal, setModal] = useState(false);
  const [existingNetworkModal, setExistingNetworkModal] = useState(false);
  const { peer } = usePeer();
  const { permission } = usePermissions();

  return (
    <>
      <Modal open={modal} onOpenChange={setModal} key={modal ? 1 : 0}>
        {modal && (
          <RouteModalContent onSuccess={() => setModal(false)} peer={peer} />
        )}
      </Modal>

      <RouteAddRoutingPeerModal
        peer={peer}
        modal={existingNetworkModal}
        setModal={setExistingNetworkModal}
      />

      <DropdownMenu modal={false}>
        <DropdownMenuTrigger
          asChild
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
          }}
        >
          <OzButton variant="primary" type="button">
            <PlusCircle size={14} />
            Add Route
            <ChevronDown size={14} />
          </OzButton>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-auto" align="end" sideOffset={10}>
          <DropdownMenuItem
            onClick={() => setModal(true)}
            disabled={!permission.routes.create}
          >
            <div className="flex items-center gap-3 pr-3">
              <span className="grid h-7 w-7 place-items-center rounded-[6px] bg-oz2-ok-bg text-oz2-ok">
                <PlusCircle size={14} />
              </span>
              <div className="flex flex-col text-left">
                <div className="text-[13px] font-medium text-oz2-text">
                  New Network Route
                </div>
                <div className="text-[11.5px] text-oz2-text-muted">
                  Create a new network route with this peer
                </div>
              </div>
            </div>
          </DropdownMenuItem>

          <DropdownMenuItem
            onClick={() => setExistingNetworkModal(true)}
            disabled={!permission.routes.update || !permission.peers.update}
          >
            <div className="flex items-center gap-3 pr-3">
              <span className="grid h-7 w-7 place-items-center rounded-[6px] bg-oz2-acc-soft text-oz2-acc-text">
                <NetworkIcon size={13} />
              </span>
              <div className="flex flex-col text-left">
                <div className="text-[13px] font-medium text-oz2-text">
                  Existing Network
                </div>
                <div className="text-[11.5px] text-oz2-text-muted">
                  Add this peer to an existing network
                </div>
              </div>
            </div>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </>
  );
}
