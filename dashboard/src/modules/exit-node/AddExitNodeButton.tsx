"use client";

import { Modal } from "@components/modal/Modal";
import { IconDirectionSign } from "@tabler/icons-react";
import { PlusCircle } from "lucide-react";
import * as React from "react";
import { useState } from "react";
import OzButton from "@/components/v2/OzButton";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Peer } from "@/interfaces/Peer";
import { ExitNodeHelpTooltip } from "@/modules/exit-node/ExitNodeHelpTooltip";
import { RouteModalContent } from "@/modules/routes/RouteModal";

type Props = {
  peer?: Peer;
  firstTime?: boolean;
};

// AddExitNodeButton — v2 paint. Renders an OzButton in the page
// toolbar; when first-time (no exit nodes yet), it surfaces a
// SetUpExitNode CTA with a guide-style icon, otherwise an Add
// Exit Node primary CTA.

export const AddExitNodeButton = ({ peer, firstTime = false }: Props) => {
  const [modal, setModal] = useState(false);
  const { permission } = usePermissions();

  return (
    <>
      <ExitNodeHelpTooltip>
        <OzButton
          variant="default"
          type="button"
          onClick={() => setModal(true)}
          disabled={!permission.routes.create}
        >
          {firstTime ? (
            <>
              <IconDirectionSign size={14} className="text-oz2-warn" />
              Set Up Exit Node
            </>
          ) : (
            <>
              <PlusCircle size={14} />
              Add Exit Node
            </>
          )}
        </OzButton>
      </ExitNodeHelpTooltip>
      <Modal open={modal} onOpenChange={setModal}>
        {modal && (
          <RouteModalContent
            onSuccess={() => setModal(false)}
            peer={peer}
            isFirstExitNode={firstTime}
            exitNode={true}
          />
        )}
      </Modal>
    </>
  );
};
