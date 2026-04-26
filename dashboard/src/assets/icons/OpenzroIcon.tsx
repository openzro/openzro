import Image from "next/image";
import * as React from "react";
import { memo } from "react";
import OpenzroLogo from "@/assets/openzro.svg";

type Props = {
  size?: number;
  className?: string;
};
function OpenzroIcon({ size = 16, className }: Props) {
  return (
    <Image
      src={OpenzroLogo}
      alt={"Openzro Icon"}
      width={size}
      className={className}
    />
  );
}

export default memo(OpenzroIcon);
