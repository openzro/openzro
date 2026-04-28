import Button from "@components/Button";
import Code from "@components/Code";
import Steps from "@components/Steps";
import TabsContentPadding, { TabsContent } from "@components/Tabs";
import { getOpenzroUpCommand, GRPC_API_ORIGIN } from "@utils/openzro";
import { DownloadIcon, PackageOpenIcon } from "lucide-react";
import Link from "next/link";
import React from "react";
import {
  findAsset,
  releaseFallbackURL,
  useLatestRelease,
} from "@/hooks/useLatestRelease";
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

// Asset preference for Windows downloads, in order:
//   1. .msi (Stage 1 of ADR-0007 — single-click installer with
//      Start menu shortcut + service install + PATH update)
//   2. openzro-ui zip (system tray UI, end-user friendly)
//   3. openzro CLI zip (terminal users)
// The release_msi CI job in .github/workflows/release-binaries.yml
// builds the .msi from client/openzro.wxs on every tag push.
const WINDOWS_MSI = /^openzro_.*_windows_amd64\.msi$/;
const WINDOWS_UI_ZIP = /^openzro-ui_.*_windows_amd64\.zip$/;
const WINDOWS_CLI_ZIP = /^openzro_.*_windows_amd64\.zip$/;

export default function WindowsTab({
  setupKey,
  showSetupKeyInfo,
  hostname,
}: Readonly<Props>) {
  const { data: release, isLoading } = useLatestRelease();
  const winAsset =
    findAsset(release, WINDOWS_MSI) ??
    findAsset(release, WINDOWS_UI_ZIP) ??
    findAsset(release, WINDOWS_CLI_ZIP);
  const downloadHref =
    winAsset?.browser_download_url ?? releaseFallbackURL(release);
  const isMsi = winAsset ? WINDOWS_MSI.test(winAsset.name) : false;

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
              <Link href={downloadHref} passHref target={"_blank"}>
                <Button variant={"primary"} disabled={isLoading}>
                  <DownloadIcon size={14} />
                  {isLoading
                    ? "Loading…"
                    : `Download Openzro${
                        release?.tag_name ? ` ${release.tag_name}` : ""
                      } (${isMsi ? "Installer" : "Windows x64"})`}
                </Button>
              </Link>
            </div>
            {isMsi ? (
              <p className={"text-xs text-nb-gray-300 mt-2"}>
                Run the installer — Windows may show a SmartScreen
                warning on first run (click <em>More info → Run anyway</em>).
                EV code-signing via SignPath is coming soon and will
                eliminate the prompt.
              </p>
            ) : (
              <p className={"text-xs text-nb-gray-300 mt-2"}>
                Extract the .zip and run <code>openzro-ui.exe</code> as
                administrator. The wintun driver is embedded in the binary
                — no separate install needed. Native MSI installer and EV
                code-signing are coming soon (tracked as part of the
                packaging epic).
              </p>
            )}
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
