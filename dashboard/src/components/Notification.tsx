import { IconCircleX } from "@tabler/icons-react";
import type { ErrorResponse } from "@utils/api";
import { cn } from "@utils/helpers";
import classNames from "classnames";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, Loader2, XIcon } from "lucide-react";
import * as React from "react";
import { useEffect, useState } from "react";
import toast, { type Toast } from "react-hot-toast";

export interface NotifyProps<T> {
  title: string;
  description: string;
  promise?: Promise<T | ErrorResponse>;
  loadingTitle?: string;
  loadingMessage?: string;
  duration?: number;
  icon?: React.ReactNode;
  backgroundColor?: string;
  preventSuccessToast?: boolean;
  errorMessages?: ErrorResponse[];
}

interface NotificationProps<T> extends NotifyProps<T> {
  t: Toast;
}
export default function Notification<T>({
  title,
  description,
  icon,
  backgroundColor,
  t,
  promise,
  loadingTitle,
  loadingMessage,
  duration = 3500,
  preventSuccessToast = false,
  errorMessages,
}: NotificationProps<T>) {
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(!!promise);

  const [toastDuration] = useState(duration);

  const [preventSuccess, setPreventSuccess] = useState(false);

  const closeToast = () => {
    setTimeout(() => {
      setLoading(false);
      toast.dismiss(t.id);
    }, toastDuration);
  };

  useEffect(() => {
    // Run the promise
    if (promise) {
      promise
        .then(() => {
          setLoading(false);
          closeToast();
          if (preventSuccessToast) setPreventSuccess(true);
        })
        .catch((e) => {
          const err = e as ErrorResponse;
          let message = err.message || "Something went wrong...";
          message = message.charAt(0).toUpperCase() + message.slice(1);
          const code: number = err.code || 418;

          if (errorMessages) {
            const errorMessage = errorMessages.find(
              (error) => error.code === code,
            );
            if (errorMessage) {
              setError(errorMessage.message);
            }
          } else {
            setError(`Code ${code}: ${message}`);
          }

          setLoading(false);
          closeToast();
        });
    } else {
      closeToast();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <AnimatePresence>
      {t.visible && !preventSuccess && (
        <motion.div
          initial={{ opacity: 1, y: -50 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: -50 }}
          className={cn(
            // v2 toast frame: bg-elev surface + soft border + md shadow.
            // 14px corner radius (rounded-oz2-card) lines up with the
            // rest of the v2 surfaces; max-w-md keeps the card legible
            // without dominating the viewport.
            "pointer-events-auto flex w-full max-w-md items-center justify-between gap-3 rounded-oz2-card border border-oz2-border bg-oz2-bg-elev px-3.5 py-3 text-oz2-text shadow-oz2-md",
          )}
        >
          <div className="flex items-center gap-3">
            <div
              className={classNames(
                "grid h-8 w-8 shrink-0 place-items-center rounded-[10px] text-oz2-text-on-acc",
                loading
                  ? "bg-oz2-bg-sunken text-oz2-text-2"
                  : error
                    ? "bg-oz2-err"
                    : backgroundColor || "bg-oz2-ok",
              )}
            >
              {loading ? (
                <Loader2 size={14} className="animate-spin" />
              ) : error ? (
                <IconCircleX size={18} />
              ) : (
                icon || <CheckIcon size={14} />
              )}
            </div>
            <div className="flex min-w-0 flex-col">
              <p className="text-[13.5px] font-semibold leading-[1.35] text-oz2-text">
                {loading ? loadingTitle || title : title}
              </p>
              <p className="mt-0.5 text-[12px] leading-[1.4] text-oz2-text-muted">
                {loading ? loadingMessage : error ? error : description}
              </p>
            </div>
          </div>

          <button
            type="button"
            onClick={() => toast.dismiss(t.id)}
            aria-label="Dismiss notification"
            className="grid h-7 w-7 shrink-0 place-items-center rounded-[8px] text-oz2-text-faint transition-colors hover:bg-oz2-hover hover:text-oz2-text"
          >
            <XIcon size={14} />
          </button>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

export function notify<T>(props: NotifyProps<T>) {
  return toast.custom((t) => <Notification {...props} t={t} />, {
    duration: Infinity,
  });
}
