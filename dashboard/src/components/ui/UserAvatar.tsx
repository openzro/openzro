import { cn, generateColorFromUser } from "@utils/helpers";
import * as React from "react";
import { useState } from "react";
import { useApplicationContext } from "@/contexts/ApplicationProvider";

type Props = {
  size?: "default" | "small" | "large" | "medium";
};

// Shared classes between the picture and the initial-letter fallback
// so the swap on load failure doesn't reflow the surrounding layout.
const SIZE_CLASSES: Record<NonNullable<Props["size"]>, string> = {
  small: "w-8 h-8 text-sm",
  medium: "w-[2.3rem] h-[2.3rem] text-base",
  default: "w-10 h-10 text-base",
  large: "w-12 h-12 text-lg",
};

export const UserAvatar = ({ size = "default" }: Props) => {
  const { user } = useApplicationContext();

  const [pictureLoaded, setPictureLoaded] = useState(true);

  const sizeClass = SIZE_CLASSES[size];

  return pictureLoaded && user?.picture ? (
    <img
      alt=""
      src={user.picture}
      onError={() => setPictureLoaded(false)}
      className={cn("rounded-full object-cover shrink-0", sizeClass)}
    />
  ) : (
    <div
      className={cn(
        "rounded-full flex items-center justify-center bg-nb-gray-900 text-openzro uppercase shrink-0 font-medium",
        sizeClass,
      )}
      style={{
        color: generateColorFromUser(user),
      }}
    >
      {user?.name?.charAt(0) || user?.id?.charAt(0)}
    </div>
  );
};
