import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import { useSWRConfig } from "swr";
import { useDialog } from "@/contexts/DialogProvider";
import { User } from "@/interfaces/User";

// useResetUserMFA removes another user's two-factor authentication, for a
// user who lost their authenticator or is locked out. The server requires
// the admin's own verified 2FA session; an admin without 2FA is told to
// set it up, and the message shows in the toast.
export default function useResetUserMFA(user: User, serviceUser: boolean) {
  const { confirm } = useDialog();
  const { mutate } = useSWRConfig();
  // ignoreError: a refusal is reported in the toast instead of replacing
  // the page with the error boundary.
  const request = useApiCall<{ disenrolled: boolean }>("/users", true);

  return async () => {
    const name = user.name || user.email || "User";
    const choice = await confirm({
      title: `Reset 2FA for '${name}'?`,
      description:
        "This removes their authenticator and backup codes and clears a lockout. They will set up 2FA again the next time it is required. Their devices stay connected.",
      confirmText: "Reset 2FA",
      cancelText: "Cancel",
      type: "warning",
    });
    if (!choice) return;
    notify({
      title: `Reset 2FA for '${name}'`,
      description: "They will set up 2FA again the next time it is required.",
      promise: request
        .del("", `/${user.id}/mfa`)
        .then(() => mutate(`/users?service_user=${serviceUser}`)),
      loadingMessage: "Resetting 2FA...",
    });
  };
}
