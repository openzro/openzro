import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@components/Tooltip";
import { Barcode, CpuIcon } from "lucide-react";
import React, { useMemo } from "react";
import {
  FaAndroid,
  FaApple,
  FaFreebsd,
  FaLinux,
  FaWindows,
} from "react-icons/fa6";
import IOSIcon from "@/assets/icons/IOSIcon";
import { getOperatingSystem } from "@/hooks/useOperatingSystem";
import { OperatingSystem } from "@/interfaces/OperatingSystem";

type Props = {
  os: string;
  serial?: string;
};
export function PeerOSCell({ os, serial }: Readonly<Props>) {
  return (
    <TooltipProvider>
      <Tooltip delayDuration={1}>
        <TooltipTrigger>
          <div
            className={
              "flex items-center gap-2 dark:text-neutral-300 text-neutral-500 hover:text-neutral-900 dark:hover:text-neutral-100 transition-all hover:bg-neutral-100 dark:hover:bg-nb-gray-800/60 py-2 px-3 rounded-md"
            }
          >
            <div
              className={"h-6 w-6 flex items-center justify-center"}
            >
              <OSLogo os={os} />
            </div>
          </div>
        </TooltipTrigger>
        <TooltipContent className={"!p-0"}>
          <div>
            <ListItem icon={<CpuIcon size={14} />} label={"OS"} value={os} />
            {serial && serial !== "" && (
              <ListItem
                icon={<Barcode size={14} />}
                label={"Serial Number"}
                value={serial}
              />
            )}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

const ListItem = ({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value: string | React.ReactNode;
}) => {
  return (
    <div
      className={
        "flex justify-between gap-5 border-b border-nb-gray-920 py-2 px-4 last:border-b-0 text-xs"
      }
    >
      <div className={"flex items-center gap-2 text-nb-gray-100 font-medium"}>
        {icon}
        {label}
      </div>
      <div className={"text-nb-gray-400"}>{value}</div>
    </div>
  );
};

export function OSLogo({ os }: { os: string }) {
  const icon = useMemo(() => {
    return getOperatingSystem(os);
  }, [os]);

  // All glyphs are now monochrome silhouettes from the Font Awesome
  // Brands set (CC BY 4.0, free) — that means a single text color
  // class drives both themes and we no longer need the brightness /
  // contrast / invert filters that the multi-color Fc* set required.
  // iOS keeps its own glyph so it reads as a phone, not a Mac.
  const cls = "text-neutral-700 dark:text-white text-lg";

  if (icon === OperatingSystem.WINDOWS) return <FaWindows className={cls} />;
  if (icon === OperatingSystem.APPLE) return <FaApple className={cls} />;
  if (icon === OperatingSystem.FREEBSD) return <FaFreebsd className={cls} />;
  if (icon === OperatingSystem.IOS)
    return (
      <IOSIcon
        className={"fill-neutral-700 dark:fill-white"}
        size={20}
      />
    );
  if (icon === OperatingSystem.ANDROID) return <FaAndroid className={cls} />;

  return <FaLinux className={cls} />;
}
