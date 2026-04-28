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
  BeerIcon,
  DownloadIcon,
  ExternalLinkIcon,
  PackageOpenIcon,
  TerminalSquareIcon,
} from "lucide-react";
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
// Asset preference for macOS, in order:
//   1. .pkg (Stage 1 of ADR-0007 — `pkgbuild` from release_pkg
//      CI job, double-click installer that drops binaries into
//      /usr/local/bin and registers the launchd daemon)
//   2. universal UI tarball (Fyne app, brew-friendly)
//   3. universal CLI tarball (terminal users)
// All assets are fat universal (amd64+arm64) — one download
// works on both Intel and Apple Silicon, no chip detection needed.
const MACOS_PKG = /^openzro_.*_darwin_universal\.pkg$/;
const MACOS_UI_UNIVERSAL = /^openzro-ui_.*_darwin_universal\.tar\.gz$/;
const MACOS_CLI_UNIVERSAL = /^openzro_.*_darwin_(universal|all)\.tar\.gz$/;

export default function MacOSTab({
  setupKey,
  showSetupKeyInfo,
  hostname,
}: Readonly<Props>) {
  const { data: release, isLoading } = useLatestRelease();
  const macAsset =
    findAsset(release, MACOS_PKG) ??
    findAsset(release, MACOS_UI_UNIVERSAL) ??
    findAsset(release, MACOS_CLI_UNIVERSAL);
  const downloadHref =
    macAsset?.browser_download_url ?? releaseFallbackURL(release);
  const isPkg = macAsset ? MACOS_PKG.test(macAsset.name) : false;
  const downloadLabel = release?.tag_name
    ? `${release.tag_name} (${isPkg ? "Installer" : "Universal"})`
    : isPkg
      ? "Installer"
      : "Universal";

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
              <Link href={downloadHref} passHref target={"_blank"}>
                <Button variant={"primary"} disabled={isLoading}>
                  <DownloadIcon size={14} />
                  {isLoading ? "Loading…" : `Download Openzro ${downloadLabel}`}
                </Button>
              </Link>
            </div>
            {isPkg ? (
              <p className={"text-xs text-nb-gray-300 mt-2"}>
                Universal installer — works on both Intel and Apple
                Silicon. Double-click the .pkg, follow the prompts, and
                the daemon registers automatically. On first run macOS
                may show a Gatekeeper warning (&quot;cannot be opened
                because Apple cannot check it&quot;) — right-click →{" "}
                <em>Open</em> to bypass once, or run{" "}
                <code>xattr -d com.apple.quarantine ~/Downloads/openzro_*.pkg</code>.
                Apple Developer ID notarization is coming soon and will
                eliminate the warning.
              </p>
            ) : (
              <p className={"text-xs text-nb-gray-300 mt-2"}>
                Universal binary works on both Intel and Apple Silicon.
                Extract the .tar.gz, then run{" "}
                <code>sudo install -m 0755 openzro-ui /usr/local/bin/</code>{" "}
                and launch from <code>/usr/local/bin/openzro-ui</code>. On
                first launch macOS may show a Gatekeeper warning — open with
                right-click → Open, or run{" "}
                <code>xattr -d com.apple.quarantine</code> on the binary.
                Signed .pkg installer + Apple Developer ID notarization are
                tracked as part of the packaging epic.
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
                Open Terminal and run Openzro{" "}
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
                    Run Openzro {!setupKey && "and log in the browser"}
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
      <Separator />
      <TabsContentPadding>
        <Accordion type="single" collapsible>
          <AccordionItem value="item-1">
            <AccordionTrigger>
              <BeerIcon size={16} /> Install manually with HomeBrew
            </AccordionTrigger>
            <AccordionContent>
              <Steps>
                <Steps.Step step={1}>
                  <p>Download and install HomeBrew</p>
                  <div className={"flex gap-4"}>
                    <Link href={"https://brew.sh/"} passHref target={"_blank"}>
                      <Button variant={"primary"}>
                        <ExternalLinkIcon size={14} />
                        HomeBrew Installation Guide
                      </Button>
                    </Link>
                  </div>
                </Steps.Step>
                <Steps.Step step={2}>
                  <p>Install the openzro CLI</p>
                  <Code codeToCopy={`brew install openzro/tap/openzro`}>
                    <Code.Line>brew install openzro/tap/openzro</Code.Line>
                  </Code>
                  <p
                    className={
                      "text-xs text-nb-gray-300 mt-2 flex items-center gap-1"
                    }
                  >
                    <BeerIcon size={12} className="opacity-60" />
                    For the system-tray GUI app, use the .pkg installer
                    above instead — a Homebrew Cask for openzro-ui will
                    ship once we publish the macOS .app bundle (tracked
                    as part of the packaging epic).
                  </p>
                </Steps.Step>
                <Steps.Step step={3}>
                  <p>Start Openzro daemon</p>
                  <Code
                    codeToCopy={[
                      `sudo brew services start openzro`,
                    ].join("\n")}
                  >
                    <Code.Comment>
                      # daemon needs root to manage WireGuard interfaces
                    </Code.Comment>
                    <Code.Line>sudo brew services start openzro</Code.Line>
                  </Code>
                </Steps.Step>
                <Steps.Step step={4} line={false}>
                  <p>
                    Run Openzro {!setupKey && "and log in the browser"}
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
