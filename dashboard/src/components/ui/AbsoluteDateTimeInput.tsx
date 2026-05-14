import { useEffect } from "react";
import { DateRange } from "@components/ui/Calendar";
import { useTimescape } from "timescape/react";

type Props = {
  value?: DateRange;
  onChange?: (range: DateRange | undefined) => void;
};
export const AbsoluteDateTimeInput = ({ value, onChange }: Props) => {
  return (
    <div className="px-4 py-4 flex flex-wrap gap-2 sm:max-w-none border-t border-oz2-border-soft">
      <div className="flex items-center gap-2 w-full justify-between">
        <div className="text-sm flex flex-col gap-1 text-oz2-text-2">
          <Time
            value={value?.from}
            onChange={(e) => {
              if (e?.getTime() === value?.from?.getTime()) return;
              onChange?.({ from: e, to: value?.to });
            }}
          />
        </div>
        <span className="text-oz2-text-faint">–</span>
        <div className="text-sm flex flex-col gap-1 text-oz2-text-2">
          <Time
            value={value?.to}
            onChange={(e) => {
              if (e?.getTime() === value?.to?.getTime()) return;
              onChange?.({ from: value?.from, to: e });
            }}
          />
        </div>
      </div>
    </div>
  );
};

const Time = ({
  value,
  onChange,
}: {
  value?: Date;
  onChange?: (date?: Date) => void;
}) => {
  // Reach into timescape's private `_manager` to drive the visible
  // inputs imperatively. Without this, presets / preset-like
  // external updates leave the visible inputs stuck on placeholders
  // because timescape's setDate skips emitting "changeDate" while
  // all registered elements are still marked `isUnset: true` (see
  // timescape/dist/react.cjs setDate_fn: bails when isCompleted()
  // returns false). The bug was previously masked because any
  // unrelated re-render — toggling theme, resizing the popover —
  // re-mounted the input refs and woke the sync up.
  const ts = useTimescape({
    date: value,
    minDate: undefined,
    maxDate: undefined,
    hour12: true,
    digits: "2-digit",
    wrapAround: false,
    snapToStep: false,
    wheelControl: true,
    disallowPartial: false,
    onChangeDate: onChange,
  });
  const { getRootProps, getInputProps } = ts;
  // _manager is not in the public typings but is stable across
  // versions (timescape ^0.x) — the cast is intentional.
  const manager = (
    ts as unknown as { _manager: { date: Date | undefined; resync: () => void } }
  )._manager;

  useEffect(() => {
    manager.date = value;
    // resync re-registers each input element with the manager,
    // recomputing `isUnset` against the now-current timestamp. After
    // this call the registered elements are marked completed, so
    // subsequent setDate calls emit "changeDate" and the inputs
    // stay in sync without any further imperative pokes.
    manager.resync();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value?.getTime()]);

  return (
    <div className={"timescape w-full"} {...getRootProps()}>
      <div className="timescape-group">
        <input {...getInputProps("years")} />
        <span className={"separator"}>/</span>
        <input {...getInputProps("months")} />
        <span className={"separator"}>/</span>
        <input {...getInputProps("days")} />
      </div>
      <span className={"separator timescape-divider"}>·</span>
      <div className="timescape-group">
        <input {...getInputProps("hours")} />
        <span className={"separator"}>:</span>
        <input {...getInputProps("minutes")} />
        <input {...getInputProps("am/pm")} className="timescape-ampm" />
      </div>
    </div>
  );
};
