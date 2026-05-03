import Card from "@components/Card";
import Paragraph from "@components/Paragraph";
import { cn } from "@utils/helpers";
import React from "react";
import Skeleton from "react-loading-skeleton";

type Props = {
  icon?: React.ReactNode;
  title?: string;
  description?: string;
  button?: React.ReactNode;
  learnMore?: React.ReactNode;
};

export default function GetStartedTest({
  icon,
  title,
  description,
  button,
  learnMore,
}: Props) {
  return (
    <div className={"px-8 mt-8"}>
      <Card className={"w-full relative overflow-hidden"}>
        <div
          className={
            // Veil over the deacmetive skeleton bars. Light mode
            // had no overlay, so the bars showed at full strength
            // and competed with the CTA. Fade from semi-transparent
            // card surface at top to opaque at bottom in both
            // themes so the skeleton reads as subtle texture, not
            // a foreground element.
            "absolute z-20 w-full h-full bg-gradient-to-b " +
            "from-neutral-50/40 to-neutral-50 " +
            "dark:from-nb-gray-950/40 dark:to-nb-gray-950"
          }
        ></div>
        <div
          className={
            "absolute w-full h-full left-0 top-0 z-10 px-5 py-3 overflow-hidden"
          }
        >
          <div className={"flex flex-col gap-2"}>
            <Skeleton className={"w-full"} height={70} duration={4} />
            <Skeleton className={"w-full"} height={70} duration={4} />
            <Skeleton className={"w-full"} height={70} duration={4} />
            <Skeleton className={"w-full"} height={70} duration={4} />
            <Skeleton className={"w-full"} height={70} duration={4} />
          </div>
        </div>
        <div className={"w-full h-full z-20 relative left-0 top-0 flex py-8"}>
          <div className={"inline-flex justify-center w-full"}>
            <div>
              <div className={"max-w-lg relative z-50"}>
                <div className={"text-center flex flex-col gap-2 p-8"}>
                  <div className={"mx-auto"}>{icon}</div>
                  <div className={"text-center"}>
                    <h1
                      className={"text-3xl font-medium max-w-lg mx-auto mt-3"}
                    >
                      {title}
                    </h1>
                    <Paragraph
                      className={cn("justify-center mt-3", button && "mb-3")}
                    >
                      {description}
                    </Paragraph>
                  </div>
                  <div>{button}</div>
                </div>
              </div>
              <Paragraph className={"text-sm justify-center pb-5 px-8"}>
                {learnMore}
              </Paragraph>
            </div>
          </div>
        </div>
      </Card>
    </div>
  );
}
