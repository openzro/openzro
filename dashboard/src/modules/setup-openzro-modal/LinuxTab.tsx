import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@components/Accordion";
import Code from "@components/Code";
import Separator from "@components/Separator";
import Steps from "@components/Steps";
import TabsContentPadding, { TabsContent } from "@components/Tabs";
import { IconBrandUbuntu } from "@tabler/icons-react";
import { getOpenzroUpCommand } from "@utils/openzro";
import { TerminalSquareIcon } from "lucide-react";
import React from "react";
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

export default function LinuxTab({
  setupKey,
  showSetupKeyInfo = false,
  hostname,
}: Readonly<Props>) {
  return (
    <TabsContent value={String(OperatingSystem.LINUX)}>
      <TabsContentPadding>
        <p className={"font-medium flex gap-3 items-center text-base"}>
          <TerminalSquareIcon size={16} />
          Install with Command-line
        </p>
        <Steps>
          <Steps.Step step={1}>
            <Code>curl -fsSL https://pkg.openzro.io/install.sh | sh</Code>
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
      </TabsContentPadding>
      <Separator />
      <TabsContentPadding>
        <Accordion type="single" collapsible>
          <AccordionItem value="item-1">
            <AccordionTrigger>
              <IconBrandUbuntu size={16} />
              Install manually on Ubuntu
            </AccordionTrigger>
            <AccordionContent>
              <Steps>
                <Steps.Step step={1}>
                  <p>Add our repository</p>
                  <Code>
                    <Code.Line>sudo apt-get update</Code.Line>
                    <Code.Line>
                      sudo apt install ca-certificates curl gnupg -y
                    </Code.Line>
                    <Code.Line>
                      curl -sSL https://pkg.openzro.io/openzro-archive-key.asc
                      | sudo gpg --dearmor --output
                      /usr/share/keyrings/openzro-archive-keyring.gpg
                    </Code.Line>
                    <Code.Line>
                      {`echo 'deb [signed-by=/usr/share/keyrings/openzro-archive-keyring.gpg] https://pkg.openzro.io/apt stable main' | sudo tee /etc/apt/sources.list.d/openzro.list`}
                    </Code.Line>
                  </Code>
                </Steps.Step>
                <Steps.Step step={2}>
                  <p>Install Openzro</p>
                  <Code
                    codeToCopy={[
                      `sudo apt-get update`,
                      `sudo apt-get install openzro`,
                      `sudo apt-get install openzro-ui`,
                    ].join("\n")}
                  >
                    <Code.Line>sudo apt-get update</Code.Line>
                    <Code.Comment># for CLI only</Code.Comment>
                    <Code.Line>sudo apt-get install openzro</Code.Line>
                    <Code.Comment># for GUI package</Code.Comment>
                    <Code.Line>sudo apt-get install openzro-ui</Code.Line>
                  </Code>
                </Steps.Step>
                <Steps.Step step={3} line={false}>
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
