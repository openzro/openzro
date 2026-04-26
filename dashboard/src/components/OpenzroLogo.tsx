import { cn } from "@utils/helpers";
import Image from "next/image";
import * as React from "react";
import OpenzroLogoMark from "@/assets/openzro.svg";
import OpenzroLogoFull from "@/assets/openzro-full.svg";

type Props = {
  size?: "default" | "large";
  mobile?: boolean;
};

const sizes = {
  default: {
    desktop: 22,
    mobile: 30,
  },
  large: {
    desktop: 24,
    mobile: 40,
  },
};

export const OpenzroLogo = ({ size = "default", mobile = true }: Props) => {
  return (
    <>
      <Image
        src={OpenzroLogoFull}
        height={sizes[size].desktop}
        alt={"Openzro Logo"}
        className={cn(mobile && "hidden md:block")}
      />
      {mobile && (
        <Image
          src={OpenzroLogoMark}
          width={sizes[size].mobile}
          alt={"Openzro Logo"}
          className={cn(mobile && "md:hidden ml-4")}
        />
      )}
    </>
  );
};
