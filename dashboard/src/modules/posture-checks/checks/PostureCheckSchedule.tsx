import { ModalClose, ModalFooter } from "@components/modal/Modal";
import { cn } from "@utils/helpers";
import {
  CalendarClock,
  ExternalLinkIcon,
  InfoIcon,
  ShieldCheck,
  ShieldXIcon,
} from "lucide-react";
import * as React from "react";
import { useMemo, useState } from "react";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import {
  OzSelect,
  OzSelectContent,
  OzSelectItem,
  OzSelectTrigger,
  OzSelectValue,
} from "@/components/v2/OzSelect";
import { OzTabs, OzTabsList, OzTabsTrigger } from "@/components/v2/OzTabs";
import {
  commonTimezones,
  ScheduleCheck,
  weekdayLabels,
} from "@/interfaces/PostureCheck";
import { PostureCheckCard } from "@/modules/posture-checks/ui/PostureCheckCard";

type Props = {
  value?: ScheduleCheck;
  onChange: (value: ScheduleCheck | undefined) => void;
  disabled?: boolean;
};

const HHMM = /^([01]\d|2[0-3]):[0-5]\d$/;

export const PostureCheckSchedule = ({ value, onChange, disabled }: Props) => {
  const [open, setOpen] = useState(false);

  return (
    <PostureCheckCard
      open={open}
      setOpen={setOpen}
      key={open ? 1 : 0}
      icon={<CalendarClock size={16} />}
      title={"Schedule"}
      description={
        "Only allow (or deny) connections inside a time-of-day window — typically business hours, after-hours lockout, or maintenance windows."
      }
      iconClass={
        "bg-amber-100 text-amber-700 dark:bg-amber-950/50 dark:text-amber-200"
      }
      modalWidthClass={"max-w-xl"}
      active={!!value}
      onReset={() => onChange(undefined)}
    >
      <CheckContent
        value={value}
        onChange={(v) => {
          onChange(v);
          setOpen(false);
        }}
        disabled={disabled}
      />
    </PostureCheckCard>
  );
};

const CheckContent = ({ value, onChange, disabled }: Props) => {
  const [action, setAction] = useState<string>(value?.action ?? "allow");
  const [days, setDays] = useState<number[]>(value?.window?.days_of_week ?? []);
  const [start, setStart] = useState<string>(value?.window?.start_time ?? "09:00");
  const [end, setEnd] = useState<string>(value?.window?.end_time ?? "18:00");
  const [timezone, setTimezone] = useState<string>(value?.timezone ?? "UTC");

  const toggleDay = (d: number) => {
    setDays((prev) =>
      prev.includes(d)
        ? prev.filter((x) => x !== d)
        : [...prev, d].sort((a, b) => a - b),
    );
  };

  const startError = useMemo(
    () => (HHMM.test(start) ? "" : "Use 24h HH:MM, e.g. 09:00"),
    [start],
  );
  const endError = useMemo(
    () => (HHMM.test(end) ? "" : "Use 24h HH:MM, e.g. 18:00"),
    [end],
  );
  const tzError = useMemo(
    () => (timezone.trim() === "" ? "Timezone is required" : ""),
    [timezone],
  );

  const wrapsMidnight =
    !startError && !endError && toMinutes(end) <= toMinutes(start);

  const hasError = !!startError || !!endError || !!tzError || disabled;

  return (
    <>

      <div className={"flex flex-col px-8 gap-5 pb-6"}>
        <div className={"flex justify-between items-start gap-10"}>
          <div>
            <OzLabel>Action</OzLabel>
            <OzHelpText className="mt-1">
              Allow: peer can connect only inside the window. Deny: peer is
              blocked inside the window.
            </OzHelpText>
          </div>
          <OzTabs
            value={action}
            onValueChange={(v) => setAction(v as "allow" | "deny")}
          >
            <OzTabsList>
              <OzTabsTrigger
                value={"allow"}
                className={
                  "gap-1.5 " +
                  // `!` overrides OzTabsTrigger's default
                  // data-[state=active]:bg-oz2-surface /
                  // text-oz2-text — Tailwind's important modifier
                  // gives the colored variant the higher specificity
                  // needed when both rules target the same selector.
                  "data-[state=active]:!bg-emerald-500/15 " +
                  "data-[state=active]:!text-emerald-700 " +
                  "data-[state=active]:!shadow-none " +
                  "dark:data-[state=active]:!text-emerald-300"
                }
              >
                <ShieldCheck size={14} />
                Allow
              </OzTabsTrigger>
              <OzTabsTrigger
                value={"deny"}
                className={
                  "gap-1.5 " +
                  "data-[state=active]:!bg-red-500/15 " +
                  "data-[state=active]:!text-red-700 " +
                  "data-[state=active]:!shadow-none " +
                  "dark:data-[state=active]:!text-red-300"
                }
              >
                <ShieldXIcon size={14} />
                Deny
              </OzTabsTrigger>
            </OzTabsList>
          </OzTabs>
        </div>

        <div>
          <OzLabel>Days of week</OzLabel>
          <OzHelpText className="mb-2">Empty = every day.</OzHelpText>
          <div className={"flex flex-wrap gap-1.5"}>
            {weekdayLabels.map((day) => {
              const selected = days.includes(day.value);
              return (
                <button
                  key={day.value}
                  type="button"
                  disabled={disabled}
                  onClick={() => toggleDay(day.value)}
                  aria-pressed={selected}
                  className={cn(
                    "inline-flex h-[34px] min-w-[52px] items-center justify-center rounded-oz2-input border px-3 text-[12.5px] font-medium transition-colors",
                    selected
                      ? "border-oz2-acc bg-oz2-acc-soft text-oz2-acc-text"
                      : "border-oz2-border bg-oz2-surface text-oz2-text-2 hover:border-oz2-border-strong hover:bg-oz2-hover",
                    disabled && "cursor-not-allowed opacity-60",
                  )}
                >
                  {day.short}
                </button>
              );
            })}
          </div>
        </div>

        <div className={"grid grid-cols-2 gap-4"}>
          <div>
            <OzLabel htmlFor={"oz-schedule-start"}>Start</OzLabel>
            <OzHelpText className="mb-2">Window opens at (24h).</OzHelpText>
            <OzInput
              id={"oz-schedule-start"}
              type={"time"}
              value={start}
              onChange={(e) => setStart(e.target.value)}
              error={startError}
              disabled={disabled}
              placeholder={"09:00"}
              mono
            />
          </div>
          <div>
            <OzLabel htmlFor={"oz-schedule-end"}>End</OzLabel>
            <OzHelpText className="mb-2">Window closes at (24h).</OzHelpText>
            <OzInput
              id={"oz-schedule-end"}
              type={"time"}
              value={end}
              onChange={(e) => setEnd(e.target.value)}
              error={endError}
              disabled={disabled}
              placeholder={"18:00"}
              mono
            />
          </div>
        </div>

        {wrapsMidnight && (
          <div
            className={
              "flex gap-2 items-start text-[11.5px] px-3 py-2 rounded-oz2-input border border-oz2-border-soft bg-oz2-bg-sunken text-oz2-text-muted"
            }
          >
            <InfoIcon size={13} className={"mt-0.5 shrink-0"} />
            <div>
              Window wraps midnight (covers {start} today through {end}{" "}
              tomorrow).
            </div>
          </div>
        )}

        <div>
          <OzLabel htmlFor={"oz-schedule-tz"}>Timezone</OzLabel>
          <OzHelpText className="mb-2">
            IANA name (e.g. America/Sao_Paulo). DST is respected; the window
            shifts with local clocks. Defaults to UTC if blank.
          </OzHelpText>
          <OzSelect
            value={timezone || "UTC"}
            onValueChange={setTimezone}
            disabled={disabled}
          >
            <OzSelectTrigger id={"oz-schedule-tz"}>
              <OzSelectValue placeholder={"UTC"} />
            </OzSelectTrigger>
            <OzSelectContent>
              {commonTimezones.map((tz) => (
                <OzSelectItem key={tz} value={tz}>
                  {tz}
                </OzSelectItem>
              ))}
            </OzSelectContent>
          </OzSelect>
        </div>
      </div>

      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={
                "https://docs.openzro.io/how-to/manage-posture-checks#schedule-check"
              }
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Schedule Check
              <ExternalLinkIcon size={12} />
            </a>
          </p>
        </div>
        <div className={"flex gap-3 w-full justify-end"}>
          <ModalClose asChild={true}>
            <OzButton variant={"default"}>Cancel</OzButton>
          </ModalClose>
          <OzButton
            variant={"primary"}
            disabled={!!hasError}
            onClick={() => {
              onChange({
                action: action as "allow" | "deny",
                window: {
                  start_time: start,
                  end_time: end,
                  ...(days.length > 0 ? { days_of_week: days } : {}),
                },
                ...(timezone.trim() && timezone.trim() !== "UTC"
                  ? { timezone: timezone.trim() }
                  : {}),
              });
            }}
          >
            Save
          </OzButton>
        </div>
      </ModalFooter>
    </>
  );
};

const toMinutes = (hhmm: string): number => {
  const [h, m] = hhmm.split(":").map(Number);
  return h * 60 + m;
};
