import Button from "@components/Button";
import Code from "@components/Code";
import InlineLink from "@components/InlineLink";
import Steps from "@components/Steps";
import TabsContentPadding, { TabsContent } from "@components/Tabs";
import { IconBrandUbuntu } from "@tabler/icons-react";
import { GRPC_API_ORIGIN } from "@utils/openzro";
import { ExternalLinkIcon } from "lucide-react";
import Link from "next/link";
import React from "react";
import { OperatingSystem } from "@/interfaces/OperatingSystem";
import { RoutingPeerSetupKeyInfo } from "@/modules/setup-openzro-modal/SetupModal";

type Props = {
  setupKey?: string;
  showSetupKeyInfo?: boolean;
  hostname?: string;
};

export default function DockerTab({
  setupKey,
  showSetupKeyInfo = false,
  hostname,
}: Readonly<Props>) {
  return (
    <TabsContent value={String(OperatingSystem.DOCKER)}>
      <TabsContentPadding>
        <p className={"font-medium flex gap-3 items-center text-base"}>
          <IconBrandUbuntu size={16} />
          Install on Ubuntu
        </p>
        <Steps>
          <Steps.Step step={1}>
            <p>Install Docker</p>
            <div className={"flex gap-4 mt-1"}>
              <Link
                href={"https://docs.docker.com/engine/install/"}
                passHref
                target={"_blank"}
              >
                <Button variant={"primary"}>
                  <ExternalLinkIcon size={14} />
                  Official Docker Installation Guide
                </Button>
              </Link>
            </div>
          </Steps.Step>
          <Steps.Step step={2}>
            <p>
              Run Openzro container
              {showSetupKeyInfo && <RoutingPeerSetupKeyInfo />}
            </p>
            <Code>
              <Code.Line>docker run --rm -d \</Code.Line>
              <Code.Line> --cap-add=NET_ADMIN \</Code.Line>
              <Code.Line>
                {" "}
                -e OZ_SETUP_KEY=
                <span className={"text-openzro"}>
                  {setupKey ?? "SETUP_KEY"}
                </span>{" "}
                \
              </Code.Line>

              {hostname && (
                <Code.Line>
                  {" "}
                  -e OZ_HOSTNAME=
                  <span className={"text-openzro"}>{`'${hostname}'`}</span> \
                </Code.Line>
              )}

              <Code.Line> -v openzro-client:/var/lib/openzro \</Code.Line>
              {GRPC_API_ORIGIN && (
                <Code.Line>
                  {" "}
                  -e OZ_MANAGEMENT_URL=
                  <span className={"text-openzro"}>{GRPC_API_ORIGIN}</span> \
                </Code.Line>
              )}
              <Code.Line> openzro/openzro:latest</Code.Line>
            </Code>
          </Steps.Step>
          <Steps.Step step={3} line={false}>
            <p>Read our documentation</p>
            <InlineLink
              href={"https://docs.openzro.io/how-to/installation/docker"}
              passHref={true}
              target={"_blank"}
            >
              Running Openzro in Docker
            </InlineLink>
          </Steps.Step>
        </Steps>
      </TabsContentPadding>
    </TabsContent>
  );
}
