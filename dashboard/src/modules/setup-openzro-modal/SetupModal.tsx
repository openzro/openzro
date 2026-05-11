"use client";

import { ModalContent, ModalFooter } from "@components/modal/Modal";
import SmallParagraph from "@components/SmallParagraph";
import { cn } from "@utils/helpers";
import {
  OzTabs as Tabs,
  OzTabsList as TabsList,
  OzTabsTrigger as TabsTrigger,
} from "@/components/v2/OzTabs";
import { ExternalLinkIcon } from "lucide-react";
import { usePathname } from "next/navigation";
import React, { useMemo } from "react";
import AppleIcon from "@/assets/icons/AppleIcon";
import DockerIcon from "@/assets/icons/DockerIcon";
import ShellIcon from "@/assets/icons/ShellIcon";
import WindowsIcon from "@/assets/icons/WindowsIcon";
import { useLocalStorage } from "@/hooks/useLocalStorage";
import useOperatingSystem from "@/hooks/useOperatingSystem";
import { OperatingSystem } from "@/interfaces/OperatingSystem";
import DockerTab from "@/modules/setup-openzro-modal/DockerTab";
import LinuxTab from "@/modules/setup-openzro-modal/LinuxTab";
import MacOSTab from "@/modules/setup-openzro-modal/MacOSTab";
import WindowsTab from "@/modules/setup-openzro-modal/WindowsTab";

// Android / iOS clients are temporarily hidden from the install
// modal: the agent apps are not yet published on the openZro side
// (no F-Droid / Play / App Store listings yet). Re-enable the
// imports + the JSX blocks below once the apps ship. The component
// files (AndroidTab.tsx / IOSTab.tsx) are kept on disk to avoid
// rewriting them when we revive this — they only contain copy +
// store badges which still apply.

type OidcUserInfo = {
  given_name?: string;
};

type Props = {
  showClose?: boolean;
  user?: OidcUserInfo;
  setupKey?: string;
  showOnlyRoutingPeerOS?: boolean;
  className?: string;
};

export default function SetupModal({
  showClose = true,
  user,
  setupKey,
  showOnlyRoutingPeerOS = false,
  className,
}: Readonly<Props>) {
  return (
    <ModalContent showClose={showClose} className={className}>
      <SetupModalContent
        user={user}
        setupKey={setupKey}
        showOnlyRoutingPeerOS={showOnlyRoutingPeerOS}
      />
    </ModalContent>
  );
}

type SetupModalContentProps = {
  user?: OidcUserInfo;
  header?: boolean;
  footer?: boolean;
  tabAlignment?: "center" | "start" | "end";
  setupKey?: string;
  showOnlyRoutingPeerOS?: boolean;
  title?: string;
  hostname?: string;
  hideDocker?: boolean;
};

export function SetupModalContent({
  user,
  header = true,
  footer = true,
  tabAlignment = "center",
  setupKey,
  showOnlyRoutingPeerOS,
  title,
  hostname,
  hideDocker = false,
}: Readonly<SetupModalContentProps>) {
  const os = useOperatingSystem();
  // useOperatingSystem can return IOS/ANDROID, but those tabs are
  // hidden right now (see import block at top of file). Map them to
  // Linux so a user opening this modal from a phone gets a clickable
  // default tab instead of an empty Tabs container.
  const safeDefaultOS = (detected: OperatingSystem): OperatingSystem =>
    detected === OperatingSystem.IOS || detected === OperatingSystem.ANDROID
      ? OperatingSystem.LINUX
      : detected;
  const [isFirstRun] = useLocalStorage<boolean>("openzro-first-run", true);
  const pathname = usePathname();
  const isInstallPage = pathname === "/install";

  const titleMessage = useMemo(() => {
    if (title) return title;

    if (isFirstRun && !isInstallPage) {
      let name = user?.given_name || "there";
      return (
        <>
          Hello {name}! 👋 <br /> It&apos;s time to add your first device.
        </>
      );
    }

    return setupKey ? "Install Openzro with Setup Key" : "Install Openzro";
  }, [isFirstRun, isInstallPage, setupKey, title, user?.given_name]);

  return (
    <>
      {header && (
        <div className={"text-center pb-5 pt-4 px-8"}>
          <h2
            className={cn(
              "max-w-lg mx-auto",
              setupKey ? "text-2xl" : "text-3xl",
            )}
          >
            {titleMessage}
          </h2>
          <p
            className={cn(
              "mx-auto mt-3 text-sm text-oz2-text-muted",
              setupKey ? "max-w-sm" : "max-w-xs",
            )}
          >
            {setupKey
              ? "To get started, install and run Openzro with the setup key as a parameter."
              : "To get started, install Openzro and log in with your email account."}
          </p>
        </div>
      )}

      <Tabs defaultValue={String(setupKey ? OperatingSystem.LINUX : safeDefaultOS(os))}>
        <div
          className={cn(
            "px-3 pb-3 pt-2",
            tabAlignment === "center" && "flex justify-center",
            tabAlignment === "end" && "flex justify-end",
          )}
        >
          <TabsList>
            <TabsTrigger value={String(OperatingSystem.LINUX)}>
              <ShellIcon
                className={
                  "fill-oz2-text-faint group-data-[state=active]/trigger:fill-oz2-acc transition-colors"
                }
              />
              Linux
            </TabsTrigger>

            <TabsTrigger value={String(OperatingSystem.WINDOWS)}>
              <WindowsIcon
                className={
                  "fill-oz2-text-faint group-data-[state=active]/trigger:fill-oz2-acc transition-colors"
                }
              />
              Windows
            </TabsTrigger>
            <TabsTrigger value={String(OperatingSystem.APPLE)}>
              <AppleIcon
                className={
                  "fill-oz2-text-faint group-data-[state=active]/trigger:fill-oz2-acc transition-colors"
                }
              />
              macOS
            </TabsTrigger>

            {/* iOS / Android tab triggers temporarily hidden — see the
                import block at the top of the file for context. */}

            {!hideDocker && (
              <TabsTrigger value={String(OperatingSystem.DOCKER)}>
                <DockerIcon
                  className={
                    "fill-oz2-text-faint group-data-[state=active]/trigger:fill-oz2-acc transition-colors"
                  }
                />
                Docker
              </TabsTrigger>
            )}
          </TabsList>
        </div>

        <LinuxTab
          setupKey={setupKey}
          showSetupKeyInfo={showOnlyRoutingPeerOS}
          hostname={hostname}
        />
        <WindowsTab
          setupKey={setupKey}
          showSetupKeyInfo={showOnlyRoutingPeerOS}
          hostname={hostname}
        />
        <MacOSTab
          setupKey={setupKey}
          showSetupKeyInfo={showOnlyRoutingPeerOS}
          hostname={hostname}
        />

        {/* AndroidTab / IOSTab content temporarily hidden — see top of file. */}

        {!hideDocker && (
          <DockerTab
            setupKey={setupKey}
            showSetupKeyInfo={showOnlyRoutingPeerOS}
            hostname={hostname}
          />
        )}
      </Tabs>
      {footer && (
        <ModalFooter variant={"setup"}>
          <div>
            <SmallParagraph>
              After that you should be connected. Add more devices to your
              network or manage your existing devices in the admin panel. If you
              have further questions check out our{" "}
              <a
                href={
                  "https://docs.openzro.io/how-to/getting-started#installation"
                }
                target={"_blank"}
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
              >
                Installation Guide
                <ExternalLinkIcon size={12} />
              </a>
            </SmallParagraph>
          </div>
        </ModalFooter>
      )}
    </>
  );
}

type SetupKeyParameterProps = {
  setupKey?: string;
};

export const SetupKeyParameter = ({ setupKey }: SetupKeyParameterProps) => {
  return (
    setupKey && (
      <>
        {" "}
        --setup-key <span className={"text-openzro"}>{setupKey}</span>
      </>
    )
  );
};

export const HostnameParameter = ({ hostname }: { hostname?: string }) => {
  return (
    hostname && (
      <>
        {" "}
        --hostname{" "}
        <span className={"text-openzro"}>
          {"'"}
          {hostname}
          {"'"}
        </span>
      </>
    )
  );
};

export const RoutingPeerSetupKeyInfo = () => {
  return (
    <div
      className={
        "flex gap-2 mt-1 items-center text-xs text-nb-gray-300 font-normal mb-1"
      }
    >
      This setup key can be used only once within the next 24 hours.
      <br />
      When expired, the same key can not be used again.
    </div>
  );
};
