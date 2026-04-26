import { cn } from "@utils/helpers";
import Image from "next/image";
import * as React from "react";
import OpenzroLogoMark from "@/assets/openzro.svg";

type Props = {
  size?: "default" | "large";
  mobile?: boolean;
};

const sizes = {
  default: {
    desktop: 28,
    mobile: 30,
    text: "text-xl",
  },
  large: {
    desktop: 36,
    mobile: 40,
    text: "text-2xl",
  },
};

// Wordmark is rendered as live markup, not baked into the SVG, so the
// middle Z can pick up the themed --oz-primary token (violet-600 light,
// violet-400 dark) and the text color follows --oz-text. See
// dashboard/CLAUDE.md "Icon and assets" and the .oz-lockup CSS in
// globals.css for the contract.
export const OpenzroLogo = ({ size = "default", mobile = true }: Props) => {
  const s = sizes[size];

  return (
    <>
      <span className={cn("oz-lockup", mobile && "hidden md:inline-flex")}>
        <Image
          src={OpenzroLogoMark}
          width={s.desktop}
          height={s.desktop}
          alt=""
          aria-hidden="true"
        />
        <span className={cn("oz-wordmark", s.text)}>
          open<span className="oz-z">Z</span>ro
        </span>
      </span>

      {mobile && (
        <Image
          src={OpenzroLogoMark}
          width={s.mobile}
          height={s.mobile}
          alt="openZro"
          className="md:hidden ml-4"
        />
      )}
    </>
  );
};
