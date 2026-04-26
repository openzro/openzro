import * as React from "react";
import { memo } from "react";
import OpenzroIcon from "@/assets/icons/OpenzroIcon";

const MemoizedOpenzroIcon = () => {
  return <OpenzroIcon size={16} />;
};

export default memo(MemoizedOpenzroIcon);
