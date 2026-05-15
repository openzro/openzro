"use client";

import * as DialogPrimitive from "@radix-ui/react-dialog";
import { DialogTriggerProps } from "@radix-ui/react-dialog";
import * as VisuallyHidden from "@radix-ui/react-visually-hidden";
import { cn } from "@utils/helpers";
import { X } from "lucide-react";
import * as React from "react";

const Modal = DialogPrimitive.Root;

const ModalTrigger = (props: DialogTriggerProps) => {
  return (
    <DialogPrimitive.Trigger
      {...props}
      onClick={(e) => {
        e.stopPropagation();
        props.onClick && props.onClick(e);
      }}
    />
  );
};

const ModalPortal = DialogPrimitive.Portal;

const ModalClose = DialogPrimitive.Close;

const ModalOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn(
      "fixed top-0 left-0 bottom-0 right-0 grid z-50 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
      "mx-auto place-items-start overflow-y-auto md:py-16",
      // Translucent ink scrim — slightly darker on dark theme so the
      // modal still pops on near-black surfaces.
      "bg-black/40 backdrop-blur-sm dark:bg-black/55",
      className,
    )}
    {...props}
  />
));
ModalOverlay.displayName = DialogPrimitive.Overlay.displayName;

type ModalContentProps = {
  showClose?: boolean;
  maxWidthClass?: string;
  // a11y title — Radix 1.1+ requires every Dialog.Content to be
  // labeled by a Dialog.Title for screen readers. Most modals already
  // render a visible <ModalTitle> inside `children`; the
  // `accessibilityTitle` prop is the fallback we emit visually
  // hidden when the caller hasn't supplied one. Defaults to "Dialog".
  accessibilityTitle?: string;
};

const ModalContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> &
    ModalContentProps
>(
  (
    {
      className,
      children,
      showClose = true,
      maxWidthClass = "max-w-3xl",
      accessibilityTitle = "Dialog",
      ...props
    },
    ref,
  ) => (
    <ModalPortal>
      <ModalOverlay>
        <DialogPrimitive.Content
          ref={ref}
          className={cn(
            "mx-auto relative top-0 z-[52] grid w-full focus:outline-0",
            // v2 modal frame: oz2-surface + oz2-border + rounded-oz2-card.
            // Layered shadow lifts the panel above the scrim.
            "border border-oz2-border bg-oz2-surface py-6 shadow-oz2-md duration-200",
            "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[state=closed]:slide-out-to-left-1 data-[state=open]:slide-in-from-left-1",
            "sm:rounded-oz2-card md:w-full",
            className,
            maxWidthClass,
          )}
          {...props}
          onClick={(e) => e.stopPropagation()}
        >
          <>
            <VisuallyHidden.Root>
              <DialogPrimitive.Title>{accessibilityTitle}</DialogPrimitive.Title>
            </VisuallyHidden.Root>
            {children}
            {showClose && (
              <DialogPrimitive.Close
                data-cy={"modal-close"}
                className="absolute right-4 top-4 z-10 grid h-7 w-7 place-items-center rounded-[8px] text-oz2-text-faint transition-colors hover:bg-oz2-hover hover:text-oz2-text focus:outline-none focus-visible:ring-2 focus-visible:ring-oz2-acc/30 disabled:pointer-events-none"
              >
                <X className="h-4 w-4" />
                <span className="sr-only">Close</span>
              </DialogPrimitive.Close>
            )}
          </>
        </DialogPrimitive.Content>
      </ModalOverlay>
    </ModalPortal>
  ),
);
ModalContent.displayName = DialogPrimitive.Content.displayName;

const SidebarModalContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> &
    ModalContentProps
>(
  (
    {
      className,
      children,
      showClose = true,
      maxWidthClass = "max-w-3xl",
      accessibilityTitle = "Dialog",
      ...props
    },
    ref,
  ) => {
    return (
      // Mirror the exact Portal > Overlay > Content nesting the
      // centred ModalContent uses — that nesting is what keeps a
      // Radix Select/Popover inside the dialog interactive. The
      // overlay overrides neutralise its centred-modal grid
      // (top placement + md:py-16) so the sheet can sit flush to
      // the right edge at full viewport height.
      <ModalPortal>
        <ModalOverlay className="place-items-stretch justify-items-end overflow-hidden p-0 md:p-0">
          <DialogPrimitive.Content
            ref={ref}
            className={cn(
              // Full-height drawer pinned to the right edge. flex-col
              // so the consumer can pin its own header/footer and let
              // only the body scroll (the body uses flex-1 + min-h-0).
              "relative z-[52] flex h-full w-full flex-col focus:outline-0",
              "border-l border-oz2-border bg-oz2-surface shadow-oz2-md duration-200",
              "data-[state=open]:animate-in data-[state=closed]:animate-out",
              "data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
              "data-[state=closed]:slide-out-to-right data-[state=open]:slide-in-from-right",
              className,
              maxWidthClass,
            )}
            {...props}
            onClick={(e) => e.stopPropagation()}
          >
            <VisuallyHidden.Root>
              <DialogPrimitive.Title>
                {accessibilityTitle}
              </DialogPrimitive.Title>
            </VisuallyHidden.Root>
            {children}
            {showClose && (
              <DialogPrimitive.Close
                data-cy={"modal-close"}
                className="absolute right-4 top-4 z-10 grid h-7 w-7 place-items-center rounded-[8px] text-oz2-text-faint transition-colors hover:bg-oz2-hover hover:text-oz2-text focus:outline-none focus-visible:ring-2 focus-visible:ring-oz2-acc/30 disabled:pointer-events-none"
              >
                <X className="h-4 w-4" />
                <span className="sr-only">Close</span>
              </DialogPrimitive.Close>
            )}
          </DialogPrimitive.Content>
        </ModalOverlay>
      </ModalPortal>
    );
  },
);
SidebarModalContent.displayName = DialogPrimitive.Content.displayName;

type ModalFooterProps = {
  variant?: "setup" | "default";
  separator?: boolean;
};
const ModalFooter = ({
  className,
  variant = "default",
  separator = true,
  ...props
}: React.HTMLAttributes<HTMLDivElement> & ModalFooterProps) => (
  <div
    className={cn(
      "border-oz2-border-soft",
      separator && "border-t",
    )}
  >
    <div
      className={cn(
        "flex flex-col-reverse sm:flex-row sm:justify-between sm:space-x-2",
        variant === "setup" && "px-6 pb-3 pt-8",
        variant === "default" && "px-8 pb-1 pt-6",
        className,
      )}
      {...props}
    />
  </div>
);
ModalFooter.displayName = "DialogFooter";

const ModalTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title
    ref={ref}
    className={cn(
      "text-lg font-semibold leading-none tracking-tight",
      className,
    )}
    {...props}
  />
));
ModalTitle.displayName = DialogPrimitive.Title.displayName;

const ModalDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description
    ref={ref}
    className={cn("text-[13px] text-oz2-text-muted", className)}
    {...props}
  />
));
ModalDescription.displayName = DialogPrimitive.Description.displayName;

export {
  Modal,
  ModalClose,
  ModalContent,
  ModalDescription,
  ModalFooter,
  ModalOverlay,
  ModalPortal,
  ModalTitle,
  ModalTrigger,
  SidebarModalContent,
};
