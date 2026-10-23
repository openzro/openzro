import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@components/Accordion";
import Button from "@components/Button";
import Code from "@components/Code";
import Separator from "@components/Separator";
import Steps from "@components/Steps";
import TabsContentPadding, { TabsContent } from "@components/Tabs";
import { getOpenzroUpCommand, GRPC_API_ORIGIN } from "@utils/openzro";
import {
  DownloadIcon,
  PackageOpenIcon,
  TerminalSquareIcon,
} from "lucide-react";
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

// Stable download URL — version-independent, mirrored to
// pkg.openzro.io by publish-packages.sh on every tag push. The .pkg
// is universal (amd64 + arm64 in one fat binary) and bundles the
// daemon + the openZro UI .app bundle that lands in /Applications/.
const MACOS_PKG_URL = "https://pkg.openzro.io/macos/openzro.pkg";

export default function MacOSTab({
  setupKey,
  showSetupKeyInfo,
  hostname,
}: Readonly<Props>) {
  const { data: release, isLoading } = useLatestRelease();
  const downloadLabel = release?.tag_name && !isLoading
    ? `${release.tag_name} (Installer)`
    : "Installer";

  return (
    <TabsContent value={String(OperatingSystem.APPLE)}>
      <TabsContentPadding>
        <p className={"font-medium flex gap-3 items-center text-base"}>
          <PackageOpenIcon size={16} />
          Install on macOS
        </p>
        <Steps>
          <Steps.Step step={1}>
            <p className={"text-sm font-light"}>
              Download the latest macOS build
            </p>
            <div className={"flex gap-4 mt-1 flex-wrap"}>
              <Link href={MACOS_PKG_URL} passHref target={"_blank"}>
                <Button variant={"primary"}>
                  <DownloadIcon size={14} />
                  Download openZro {downloadLabel}
                </Button>
              </Link>
            </div>
            <p className={"text-xs text-nb-gray-300 mt-2"}>
              Universal installer — works on both Intel and Apple
              Silicon. Double-click the .pkg, follow the prompts, and
              the daemon registers as a LaunchDaemon while the
              openZro UI lands in <code>/Applications/openZro UI.app</code>.
              The installer is signed with an Apple Developer ID and
              notarized, so macOS opens it without a Gatekeeper warning.
            </p>
          </Steps.Step>

          {GRPC_API_ORIGIN && (
            <Steps.Step step={2}>
              <p>
                {`Click on "Settings" then "Advanced Settings" from the openZro icon in your system tray and enter the following "Management URL"`}
              </p>
              <Code>
                <Code.Line>{GRPC_API_ORIGIN}</Code.Line>
              </Code>
            </Steps.Step>
          )}

          {setupKey ? (
            <Steps.Step step={GRPC_API_ORIGIN ? 3 : 2} line={false}>
              <p>
                Open Terminal and run openZro{" "}
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
                  Click on "Connect" from the openZro icon in your system tray
                </p>
              </Steps.Step>
              <Steps.Step step={GRPC_API_ORIGIN ? 4 : 3} line={false}>
                <p>Sign up using your email address</p>
              </Steps.Step>
            </>
          )}
        </Steps>
      </TabsContentPadding>
      <Separator />
      <TabsContentPadding>
        <Accordion type="single" collapsible>
          <AccordionItem value="item-1">
            <AccordionTrigger>
              <TerminalSquareIcon size={16} />
              Install manually with Terminal
            </AccordionTrigger>
            <AccordionContent>
              <Steps>
                <Steps.Step step={1}>
                  <Code>
                    curl -fsSL https://pkg.openzro.io/install.sh | sh
                  </Code>
                </Steps.Step>
                <Steps.Step step={2} line={false}>
                  <p>
                    Run openZro {!setupKey && "and log in the browser"}
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
              </Steps>
            </AccordionContent>
          </AccordionItem>
        </Accordion>
      </TabsContentPadding>
    </TabsContent>
  );
}
