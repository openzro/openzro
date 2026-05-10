"use client";

import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { AlertCircle, AlertTriangle } from "lucide-react";
import React, { useCallback, useRef, useState } from "react";
import OzButton from "@/components/v2/OzButton";

type Props = {
  children: React.ReactNode;
};

const DialogContext = React.createContext(
  {} as {
    confirm: (data: DialogOptions) => Promise<boolean>;
  },
);

type DialogOptions = {
  title?: string | React.ReactNode;
  description?: string | React.ReactNode;
  confirmText?: string;
  cancelText?: string;
  type?: "default" | "warning" | "danger" | "center";
  children?: React.ReactNode;
  maxWidthClass?: string;
};

// DialogProvider — global confirm dialog. v2 paint:
//   default  blue icon (info) + primary OzButton confirm
//   warning  openzro icon (alert circle) + primary OzButton confirm
//   danger   red icon (alert triangle) + dedicated err-paint button
//   center   no icon, centered text (used for terse confirmations)
//
// Confirm button on the right, Cancel on the left. Same Promise<bool>
// API the rest of the app calls.

export default function DialogProvider({ children }: Props) {
  const [state, setState] = useState({
    isOpen: false,
  });
  const [dialogOptions, setDialogOptions] = useState<DialogOptions>();
  const fn = useRef<Function>();

  const confirm = useCallback((data: DialogOptions): Promise<boolean> => {
    return new Promise((resolve) => {
      setState({ isOpen: true });
      setDialogOptions(data);
      fn.current = (choice: boolean) => {
        resolve(choice);
        setDialogOptions(undefined);
        setState({ isOpen: false });
      };
    });
  }, []);

  const dialogTypes = {
    default: "",
    warning: <AlertCircle size={18} />,
    danger: <AlertTriangle size={18} />,
    center: "",
  };

  const isDanger = dialogOptions?.type === "danger";

  return (
    <DialogContext.Provider value={{ confirm }}>
      {children}
      <Modal
        open={state.isOpen}
        onOpenChange={(open) => fn.current && fn.current(open)}
      >
        {dialogOptions && (
          <ModalContent
            maxWidthClass={dialogOptions.maxWidthClass || "max-w-[440px]"}
            showClose={false}
          >
            <ModalHeader
              center={dialogOptions.type == "center"}
              title={dialogOptions.title || "Confirmation"}
              margin={"mt-1"}
              description={
                dialogOptions.description ||
                "Are you sure you want to continue? This action cannot be undone."
              }
              icon={dialogTypes[dialogOptions.type || "default"]}
              color={
                dialogOptions.type == "default"
                  ? "blue"
                  : dialogOptions.type == "warning"
                    ? "openzro"
                    : "red"
              }
              className="px-7"
            />

            {dialogOptions.children && (
              <div className="px-7 pt-0">{dialogOptions.children}</div>
            )}

            <ModalFooter
              className="items-center gap-2 pt-5"
              separator={false}
            >
              <ModalClose asChild>
                <OzButton
                  variant="default"
                  type="button"
                  className="w-full"
                  tabIndex={-1}
                  data-cy="confirmation.cancel"
                  onClick={() => fn.current && fn.current(false)}
                >
                  {dialogOptions.cancelText || "Cancel"}
                </OzButton>
              </ModalClose>
              <OzButton
                variant="primary"
                type="button"
                className={
                  "w-full " +
                  (isDanger
                    ? "!bg-oz2-err !text-oz2-text-on-acc hover:!bg-oz2-err/90 !shadow-none !border-transparent"
                    : "")
                }
                data-cy="confirmation.confirm"
                onClick={() => fn.current && fn.current(true)}
              >
                {dialogOptions.confirmText || "Confirm"}
              </OzButton>
            </ModalFooter>
          </ModalContent>
        )}
      </Modal>
    </DialogContext.Provider>
  );
}

export const useDialog = () => React.useContext(DialogContext);
