import { ShieldCheck } from "lucide-react";
import * as React from "react";
import { OzTabsTrigger } from "@/components/v2/OzTabs";

type Props = {
  disabled?: boolean;
};

export const PostureCheckTabTrigger = ({ disabled = false }: Props) => {
  return (
    <OzTabsTrigger value={"posture_checks"} disabled={disabled}>
      <ShieldCheck size={16} />
      Posture Checks
    </OzTabsTrigger>
  );
};
