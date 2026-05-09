import { cn } from "@utils/helpers";
import * as React from "react";
import { FaApple, FaWindows } from "react-icons/fa6";
import RoundedFlag from "@/assets/countries/RoundedFlag";
import OpenzroIcon from "@/assets/icons/OpenzroIcon";

type Props = {
  children: React.ReactNode;
  className?: string;
};

export const PostureCheckIcons = () => {
  return (
    <div className={"flex items-center justify-center -space-x-2"}>
      <Circle className={"top-2"}>
        <FaApple className={"text-neutral-700 dark:text-white text-md"} />
      </Circle>
      <Circle className={"top-1"}>
        <div
          className={
            "h-6 w-6 overflow-hidden rounded-full flex items-center justify-center"
          }
        >
          <RoundedFlag country="de" />
        </div>
      </Circle>
      <Circle className={"z-[3]"}>
        <OpenzroIcon size={18} />
      </Circle>
      <Circle className={"top-1 z-[2]"}>
        <div
          className={
            "h-6 w-6 overflow-hidden rounded-full flex items-center justify-center"
          }
        >
          <RoundedFlag country="us" />
        </div>
      </Circle>
      <Circle className={"z-[1] top-2 "}>
        <FaWindows className={"text-white text-md"} />
      </Circle>
    </div>
  );
};

const Circle = ({ children, className }: Props) => {
  return (
    <div
      className={cn(
        "h-10 w-10 rounded-full flex items-center justify-center relative border-2",
        "bg-neutral-100 border-white",
        "dark:bg-nb-gray-900 dark:border-nb-gray",
        className,
      )}
    >
      {children}
    </div>
  );
};
