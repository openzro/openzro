import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { IconCornerDownLeft } from "@tabler/icons-react";
import { trim } from "lodash";
import * as React from "react";
import { useMemo, useState } from "react";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";

type Props = {
  initialName: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess: (name: string) => void;
};
export const EditGroupNameModal = ({
  initialName,
  onOpenChange,
  open,
  onSuccess,
}: Props) => {
  const [name, setName] = useState(initialName);
  const isDisabled = useMemo(() => {
    if (name === initialName) return true;
    const trimmedName = trim(name);
    return trimmedName.length === 0;
  }, [name, initialName]);

  return (
    <Modal open={open} onOpenChange={onOpenChange}>
      <ModalContent maxWidthClass={"max-w-md"}>
        <form>
          <ModalHeader
            title={"Edit Group Name"}
            description={"Set an easily identifiable name for your group."}
            color={"blue"}
          />

          <div className={"p-default flex flex-col gap-4"}>
            <OzInput
              placeholder={"e.g., AWS Servers"}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <ModalFooter className={"items-center"} separator={false}>
            <div className={"flex gap-3 w-full justify-end"}>
              <ModalClose asChild={true}>
                <OzButton variant={"default"} className={"w-full"}>
                  Cancel
                </OzButton>
              </ModalClose>

              <OzButton
                variant={"primary"}
                className={"w-full"}
                onClick={() => onSuccess(name)}
                disabled={isDisabled}
                type={"submit"}
              >
                Confirm
                <IconCornerDownLeft size={16} />
              </OzButton>
            </div>
          </ModalFooter>
        </form>
      </ModalContent>
    </Modal>
  );
};
