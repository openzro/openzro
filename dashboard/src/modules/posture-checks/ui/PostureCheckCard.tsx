import FullTooltip from "@components/FullTooltip";
import { Modal, ModalContent } from "@components/modal/Modal";
import { cn } from "@utils/helpers";
import { ScaleIcon } from "lucide-react";
import * as React from "react";
import { useDialog } from "@/contexts/DialogProvider";

// PostureCheckCard — row used inside PostureCheckModal for each
// posture-check type (OS, version, geolocation, etc). v2 paint:
// soft tinted icon tile (no shadow / no bright gradient), oz2 row
// hover, On/Off pill in oz2-acc-soft vs oz2-bg-sunken.

export const PostureCheckCard = ({
  children,
  title,
  description,
  icon,
  iconClass = "bg-oz2-acc-soft text-oz2-acc-text",
  modalWidthClass = "max-w-xl",
  onClose,
  open,
  setOpen,
  active,
  onReset,
  license,
}: {
  children?: React.ReactNode;
  title?: string;
  description?: string;
  iconClass?: string;
  icon?: React.ReactNode;
  modalWidthClass?: string;
  onClose?: () => void;
  open: boolean;
  setOpen: (open: boolean) => void;
  onReset?: () => void;
  active?: boolean;
  license?: React.ReactNode;
}) => {
  const { confirm } = useDialog();

  const handleReset = async () => {
    const reset = await confirm({
      title: `Disable this check?`,
      description:
        "Are you sure you want to disable this check? All settings of this check will be lost.",
      confirmText: "Disable",
      cancelText: "Cancel",
      type: "danger",
    });
    if (reset) onReset?.();
  };

  const licenseToolTip = (
    <FullTooltip content={license}>
      <ScaleIcon
        size={13}
        className={
          "cursor-pointer text-oz2-text-faint transition-colors hover:text-oz2-text-2 relative -top-[1px]"
        }
      />
    </FullTooltip>
  );

  const tile = (
    <div
      className={cn(
        "grid h-9 w-9 shrink-0 place-items-center rounded-[10px] border border-oz2-border-soft select-none",
        iconClass,
      )}
    >
      {icon}
    </div>
  );

  return (
    <div className={"w-full"}>
      <div
        onClick={() => setOpen(true)}
        className={cn(
          "flex w-full cursor-pointer items-center gap-4 rounded-oz2-input border border-transparent px-4 py-3 transition-colors",
          "hover:bg-oz2-hover hover:border-oz2-border-soft",
        )}
      >
        {tile}
        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-2 text-[13.5px] font-medium text-oz2-text">
            <span className="inline-flex items-center gap-2">
              {title}
              {license && licenseToolTip}
            </span>
          </div>
          <div className="mt-0.5 text-[12px] leading-[1.45] text-oz2-text-muted">
            {description}
          </div>
        </div>
        <span
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
            if (active) handleReset().then();
            else setOpen(true);
          }}
          className={cn(
            "inline-flex w-[46px] items-center justify-center gap-1 rounded-full px-2 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-[0.06em] transition-colors",
            active
              ? "bg-oz2-acc-soft text-oz2-acc-text hover:bg-oz2-acc-soft-2"
              : "border border-oz2-border-soft bg-oz2-bg-sunken text-oz2-text-muted hover:bg-oz2-hover",
          )}
        >
          <span
            aria-hidden
            className={cn(
              "inline-block h-1.5 w-1.5 rounded-full",
              active ? "bg-oz2-acc" : "bg-oz2-text-faint/70",
            )}
          />
          {active ? "On" : "Off"}
        </span>
      </div>

      <Modal
        open={open}
        onOpenChange={(open) => {
          setOpen(open);
          if (onClose && !open) {
            onClose();
          }
        }}
        key={open ? 1 : 0}
      >
        <ModalContent
          maxWidthClass={cn("relative", modalWidthClass)}
          showClose={true}
        >
          <div className="flex items-center gap-4 px-8 pb-5">
            {tile}
            <div className="min-w-0 pr-10">
              <div className="flex items-center gap-2 text-[14px] font-semibold text-oz2-text">
                {title}
                {license && licenseToolTip}
              </div>
              <div className="mt-0.5 text-[12.5px] text-oz2-text-muted">
                {description}
              </div>
            </div>
          </div>

          {children}
        </ModalContent>
      </Modal>
    </div>
  );
};
