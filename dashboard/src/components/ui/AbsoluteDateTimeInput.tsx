import { useEffect } from "react";
import { DateRange } from "react-day-picker";
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
  const { getRootProps, getInputProps, options, update } = useTimescape({
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

  useEffect(() => {
    if (options.date?.getTime() !== value?.getTime()) {
      update({ ...options, date: value });
    }
  }, [value]);

  return (
    <div className={"timescape w-full"} {...getRootProps()}>
      <div>
        <input {...getInputProps("years")} />
        <span className={"separator"}>/</span>
        <input {...getInputProps("months")} />
        <span className={"separator"}>/</span>
        <input {...getInputProps("days")} />
      </div>
      <span className={"separator px-1"}>⋆</span>
      <div>
        <input {...getInputProps("hours")} />
        <span className={"separator"}>:</span>
        <input {...getInputProps("minutes")} />
        <span className={"separator"}>:</span>
        <input {...getInputProps("seconds")} />
        <input {...getInputProps("am/pm")} />
      </div>
    </div>
  );
};
