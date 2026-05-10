import SquareIcon, { IconVariant } from "@components/SquareIcon";
import { cn } from "@utils/helpers";
import React from "react";

interface Props extends IconVariant {
  icon?: React.ReactNode;
  title: string | React.ReactNode;
  description: string | React.ReactNode;
  className?: string;
  margin?: string;
  truncate?: boolean;
  children?: React.ReactNode;
  center?: boolean;
}

// ModalHeader — title + description row at the top of a modal,
// optionally prefixed by a SquareIcon glyph. Paint adopted to v2
// tokens (oz2-text title, oz2-text-muted description).

export default function ModalHeader({
  icon,
  title,
  description,
  color = "openzro",
  className = "pb-6 px-8",
  margin = "mt-1",
  truncate = false,
  children,
  center,
}: Props) {
  return (
    <div className={cn(className, "min-w-0 relative z-[1]")}>
      <div className="flex items-start gap-4 min-w-0">
        {icon && <SquareIcon color={color} icon={icon} />}
        <div className={cn("min-w-0", center && "text-center")}>
          <h2
            className={cn(
              "text-[16px] font-semibold leading-[1.4] tracking-tight text-oz2-text my-0",
              center && "text-center",
            )}
          >
            {title}
          </h2>
          {children ? (
            <>{children}</>
          ) : (
            <p
              className={cn(
                "text-[13px] leading-[1.55] text-oz2-text-muted",
                margin,
                truncate && "truncate",
              )}
            >
              {description}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
