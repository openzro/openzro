import { notify } from "@components/Notification";
import { useSWRConfig } from "swr";
import { useDialog } from "@/contexts/DialogProvider";
import { usePeer } from "@/contexts/PeerProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";

// usePeerApproval approves the peer of the surrounding PeerProvider after a
// confirmation. Peer approval holds a new peer until an admin admits it;
// the peer then connects to the others on its next sync.
export function usePeerApproval() {
  const { peer, update } = usePeer();
  const { confirm } = useDialog();
  const { mutate } = useSWRConfig();
  const { permission } = usePermissions();

  const pending = !!peer?.approval_required;
  const canApprove = permission.peers.update;

  const approve = async () => {
    if (!pending || !canApprove) return;
    const choice = await confirm({
      title: `Approve peer '${peer.name}'?`,
      description:
        "The peer will be able to connect to the peers its access policies allow.",
      confirmText: "Approve",
      cancelText: "Cancel",
      type: "default",
    });
    if (!choice) return;
    notify({
      title: `Peer ${peer.name} approved`,
      description: "The peer can now connect to other peers.",
      promise: update({ approval_required: false }).then(() => {
        mutate("/peers");
        mutate(`/peers/${peer.id}`);
        mutate("/groups");
      }),
      loadingMessage: "Approving peer...",
    });
  };

  return { pending, canApprove, approve };
}
