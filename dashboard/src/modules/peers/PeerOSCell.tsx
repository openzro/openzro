import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@components/Tooltip";
import { Barcode, CpuIcon } from "lucide-react";
import Image from "next/image";
import React, { useMemo } from "react";
import { FaWindows } from "react-icons/fa6";
import { FcAndroidOs, FcLinux } from "react-icons/fc";
import IOSIcon from "@/assets/icons/IOSIcon";
import AppleLogo from "@/assets/os-icons/apple.svg";
import FreeBSDLogo from "@/assets/os-icons/FreeBSD.png";
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
              className={
                "h-6 w-6 flex items-center justify-center grayscale brightness-[100%] contrast-[40%]"
              }
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

  // The Apple + iOS marks ship as solid-color SVGs that read as white
  // on a dark surface but vanish on the light theme. We invert the
  // glyph in light mode so it renders as black against light surfaces
  // and stays untouched in dark mode. Windows + Linux + Android use
  // explicit text colors with the same dark/light pair so they remain
  // legible regardless of the active theme.
  if (icon === OperatingSystem.WINDOWS)
    return (
      <FaWindows className={"text-neutral-700 dark:text-white text-lg"} />
    );
  if (icon === OperatingSystem.APPLE)
    return (
      <Image
        src={AppleLogo}
        alt={""}
        width={14}
        className={"invert dark:invert-0"}
      />
    );
  if (icon === OperatingSystem.FREEBSD)
    return <Image src={FreeBSDLogo} alt={""} width={18} />;
  if (icon === OperatingSystem.IOS)
    return (
      <IOSIcon
        className={"fill-neutral-700 dark:fill-white"}
        size={20}
      />
    );
  if (icon === OperatingSystem.ANDROID)
    return (
      // brightness:200 brightens the multi-coloured FcAndroidOs SVG so
      // it pops on a dark surface; on light the same filter washes it
      // out into near-white. Apply it only when the html.dark class is
      // active.
      <FcAndroidOs
        className={"text-neutral-700 dark:text-white text-2xl dark:brightness-200"}
      />
    );

  return (
    <FcLinux
      className={"text-neutral-700 dark:text-white text-2xl dark:brightness-150"}
    />
  );
}
