"use client";

import { ModalContent, ModalFooter } from "@components/modal/Modal";
import { notify } from "@components/Notification";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { useApiCall } from "@utils/api";
import { validator } from "@utils/helpers";
import { MailIcon, User2, UserPlus } from "lucide-react";
import React, { useMemo, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import { User } from "@/interfaces/User";
import useGroupHelper from "@/modules/groups/useGroupHelper";
import { isValidLocalPassword } from "@/modules/users/local/localPassword";
import LocalPasswordFields from "@/modules/users/local/LocalPasswordFields";

type Props = {
  onSuccess: () => void;
};

// LocalUserCreateModalContent creates a password-backed local user
// through POST /api/users/local (ADR-0026 D1). The server applies the
// email-domain allowlist and the password policy; its errors surface in
// the toast.
export function LocalUserCreateModalContent({ onSuccess }: Readonly<Props>) {
  const request = useApiCall<User>("/users/local");
  const { mutate } = useSWRConfig();

  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [selectedGroups, setSelectedGroups, { save: saveGroups }] =
    useGroupHelper({ initial: [] });

  const isDisabled = useMemo(
    () =>
      submitting ||
      !validator.isValidEmail(email) ||
      !isValidLocalPassword(password, confirm),
    [submitting, email, password, confirm],
  );

  const create = async () => {
    setSubmitting(true);
    const groups = await saveGroups();
    notify({
      title: "Local user",
      description: `${email} can now sign in with the password you set.`,
      promise: request
        .post({
          email,
          name,
          password,
          auto_groups: groups
            .map((g) => g.id)
            .filter((id): id is string => Boolean(id)),
        })
        .then(() => {
          mutate("/users?service_user=false");
          onSuccess();
        })
        .finally(() => setSubmitting(false)),
      loadingMessage: "Creating the local user...",
    });
  };

  return (
    <ModalContent maxWidthClass="max-w-md" showClose={true}>
      <div className="mt-2 flex flex-col items-center text-center">
        <h2 className="my-0 text-lg">Create local user</h2>
        <p className="max-w-xs text-sm text-oz2-text-muted">
          The user signs in with this email and password. No external identity
          provider is involved.
        </p>
      </div>

      <div className="mt-4 flex flex-col gap-5 px-8 py-3">
        <div className="flex flex-col gap-3">
          <OzInput
            data-cy="local-user-name"
            prefix={<User2 size={16} />}
            placeholder="Name (optional)"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <OzInput
            type="email"
            autoComplete="off"
            data-cy="local-user-email"
            prefix={<MailIcon size={16} />}
            placeholder="user@example.com"
            value={email}
            onChange={(e) => setEmail(e.target.value.trim())}
          />
        </div>

        <LocalPasswordFields
          password={password}
          confirm={confirm}
          onPasswordChange={setPassword}
          onConfirmChange={setConfirm}
        />

        <div className="mb-2">
          <OzLabel>Auto-assigned groups</OzLabel>
          <OzHelpText className="mb-2">
            Groups will be assigned to peers added by this user.
          </OzHelpText>
          <PeerGroupSelector
            onChange={setSelectedGroups}
            values={selectedGroups}
            hideAllGroup={true}
          />
        </div>
      </div>

      <ModalFooter className="items-center">
        <OzButton
          variant="primary"
          className="w-full"
          data-cy="local-user-create-submit"
          disabled={isDisabled}
          onClick={create}
        >
          <UserPlus size={16} />
          Create user
        </OzButton>
      </ModalFooter>
    </ModalContent>
  );
}
