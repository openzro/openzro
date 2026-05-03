import { cn } from "@utils/helpers";
import { useRouter } from "next/navigation";
import * as React from "react";
import { useMemo } from "react";
import { useLoggedInUser, useUsers } from "@/contexts/UsersProvider";
import { Peer } from "@/interfaces/Peer";
import ActiveInactiveRow from "@/modules/common-table-rows/ActiveInactiveRow";
import { ExitNodePeerIndicator } from "@/modules/exit-node/ExitNodePeerIndicator";

type Props = {
  peer: Peer;
  linkToPeer?: boolean;
};
export default function PeerNameCell({ peer, linkToPeer = true }: Props) {
  const { users } = useUsers();
  const router = useRouter();
  const { isOwnerOrAdmin } = useLoggedInUser();

  const userOfPeer = useMemo(() => {
    return users?.find((user) => user.id === peer.user_id);
  }, [users, peer.user_id]);

  const displayUserEmailOrName = userOfPeer?.email || userOfPeer?.name;
  const displayUserId = userOfPeer?.id || peer?.user_id;

  return (
    <div>
      <div
        className={cn(
          "flex items-center max-w-[300px] gap-2 transition-all py-2 px-3 rounded-md",
          "text-neutral-500 dark:text-neutral-300",
          linkToPeer &&
            "cursor-pointer " +
              "hover:text-neutral-900 dark:hover:text-neutral-100 " +
              "hover:bg-neutral-100 dark:hover:bg-nb-gray-800/60",
        )}
        data-testid="peer-name-cell"
        aria-label={`View details of peer ${peer.name}`}
        onClick={() => linkToPeer && router.push("/peer?id=" + peer.id)}
      >
        <ActiveInactiveRow
          active={peer.connected}
          text={peer.name}
          additionalInfo={
            isOwnerOrAdmin && <ExitNodePeerIndicator peer={peer} />
          }
        >
          <div
            className={
              "text-neutral-500 dark:text-nb-gray-400 font-light truncate"
            }
          >
            {displayUserEmailOrName ||
              (displayUserId && `user: ${displayUserId}`)}
          </div>
        </ActiveInactiveRow>
      </div>
    </div>
  );
}
