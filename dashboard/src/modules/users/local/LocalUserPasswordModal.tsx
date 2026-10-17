"use client";

import { ModalContent, ModalFooter } from "@components/modal/Modal";
import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import { KeyRound } from "lucide-react";
import React, { useState } from "react";
import OzButton from "@/components/v2/OzButton";
import { User } from "@/interfaces/User";
import { isValidLocalPassword } from "@/modules/users/local/localPassword";
import LocalPasswordFields from "@/modules/users/local/LocalPasswordFields";

type Props = {
  user: User;
  onSuccess: () => void;
};

// LocalUserPasswordModalContent replaces a local user's password through
// POST /api/users/{id}/password (ADR-0026 D1). The server also revokes
// the user's refresh tokens, but access tokens already issued stay valid
// until they expire and connected peers stay up (ADR-0024 D6); the copy
// says so and points to Deactivate for cutting access now.
export function LocalUserPasswordModalContent({
  user,
  onSuccess,
}: Readonly<Props>) {
  const request = useApiCall<User>("/users");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const reset = () => {
    setSubmitting(true);
    notify({
      title: "Password reset",
      description: `${
        user.email || user.name
      } must use the new password for future sign-ins.`,
      promise: request
        .post({ password }, `/${user.id}/password`)
        .then(() => onSuccess())
        .finally(() => setSubmitting(false)),
      loadingMessage: "Resetting the password...",
    });
  };

  return (
    <ModalContent maxWidthClass="max-w-md" showClose={true}>
      <div className="mt-2 flex flex-col items-center text-center">
        <h2 className="my-0 text-lg">Reset password</h2>
        <p className="max-w-xs text-sm text-oz2-text-muted">
          Set a new password for {user.email || user.name}. Future sign-ins need
          it, but access tokens already issued stay valid until they expire, and
          connected devices stay connected. Deactivate the user to cut both now.
        </p>
      </div>

      <div className="mt-4 px-8 py-3">
        <LocalPasswordFields
          password={password}
          confirm={confirm}
          onPasswordChange={setPassword}
          onConfirmChange={setConfirm}
        />
      </div>

      <ModalFooter className="items-center">
        <OzButton
          variant="primary"
          className="w-full"
          data-cy="local-user-reset-submit"
          disabled={submitting || !isValidLocalPassword(password, confirm)}
          onClick={reset}
        >
          <KeyRound size={16} />
          Reset password
        </OzButton>
      </ModalFooter>
    </ModalContent>
  );
}
