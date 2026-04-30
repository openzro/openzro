import Button from "@components/Button";
import Code from "@components/Code";
import Steps from "@components/Steps";
import TabsContentPadding, { TabsContent } from "@components/Tabs";
import { getOpenzroUpCommand, GRPC_API_ORIGIN } from "@utils/openzro";
import { DownloadIcon, PackageOpenIcon } from "lucide-react";
import Link from "next/link";
import React from "react";
import { useLatestRelease } from "@/hooks/useLatestRelease";
import { OperatingSystem } from "@/interfaces/OperatingSystem";
import {
  HostnameParameter,
  RoutingPeerSetupKeyInfo,
  SetupKeyParameter,
} from "@/modules/setup-openzro-modal/SetupModal";

type Props = {
  setupKey?: string;
  showSetupKeyInfo?: boolean;
  hostname?: string;
};

// Stable download URL — version-independent. publish-packages.sh
// copies the latest signed MSI to this path on every tag push, so
// the dashboard never has to hit the GitHub API to find a download.
// The MSI bundles CLI + tray UI + wintun driver (one click → all of
// openZro on Windows).
const WINDOWS_MSI_URL = "https://pkg.openzro.io/windows/openzro.msi";

export default function WindowsTab({
  setupKey,
  showSetupKeyInfo,
  hostname,
}: Readonly<Props>) {
  const { data: release, isLoading } = useLatestRelease();

  return (
    <TabsContent value={String(OperatingSystem.WINDOWS)}>
      <TabsContentPadding>
        <p className={"font-medium flex gap-3 items-center text-base"}>
          <PackageOpenIcon size={16} />
          Install on Windows
        </p>
        <Steps>
          <Steps.Step step={1}>
            <p>Download the latest Windows build</p>
            <div className={"flex gap-4 mt-1"}>
              <Link href={WINDOWS_MSI_URL} passHref target={"_blank"}>
                <Button variant={"primary"}>
                  <DownloadIcon size={14} />
                  Download openZro
                  {release?.tag_name && !isLoading ? ` ${release.tag_name}` : ""}
                  {" "}(Installer)
                </Button>
              </Link>
            </div>
            <p className={"text-xs text-nb-gray-300 mt-2"}>
              The .msi bundles the daemon, the system-tray UI, and the
              wintun driver — one click installs everything. Windows may
              show a SmartScreen warning on first run (click{" "}
              <em>More info → Run anyway</em>); EV code-signing via
              SignPath is on the way and will eliminate the prompt.
            </p>
          </Steps.Step>

          {GRPC_API_ORIGIN && (
            <Steps.Step step={2}>
              <p>
                {`Click on "Settings" then "Advanced Settings" from the Openzro icon in your system tray and enter the following "Management URL"`}
              </p>
              <Code>
                <Code.Line>{GRPC_API_ORIGIN}</Code.Line>
              </Code>
            </Steps.Step>
          )}

          {setupKey ? (
            <Steps.Step step={GRPC_API_ORIGIN ? 3 : 2} line={false}>
              <p>
                Open Command-line and run Openzro{" "}
                {showSetupKeyInfo && <RoutingPeerSetupKeyInfo />}
              </p>

              <Code>
                <Code.Line>
                  {getOpenzroUpCommand()}
                  <SetupKeyParameter setupKey={setupKey} />
                  <HostnameParameter hostname={hostname} />
                </Code.Line>
              </Code>
            </Steps.Step>
          ) : (
            <>
              <Steps.Step step={GRPC_API_ORIGIN ? 3 : 2}>
                <p>
                  {/* eslint-disable-next-line react/no-unescaped-entities */}
                  Click on "Connect" from the Openzro icon in your system tray
                </p>
              </Steps.Step>
              <Steps.Step step={GRPC_API_ORIGIN ? 4 : 3} line={false}>
                <p>Sign up using your email address</p>
              </Steps.Step>
            </>
          )}
        </Steps>
      </TabsContentPadding>
    </TabsContent>
  );
}
