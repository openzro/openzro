import Button from "@components/Button";
import FancyToggleSwitch from "@components/FancyToggleSwitch";
import HelpText from "@components/HelpText";
import InlineLink from "@components/InlineLink";
import { Label } from "@components/Label";
import { ModalClose, ModalFooter } from "@components/modal/Modal";
import Paragraph from "@components/Paragraph";
import { SelectDropdown } from "@components/select/SelectDropdown";
import useFetchApi from "@utils/api";
import { ExternalLinkIcon, ShieldHalf } from "lucide-react";
import * as React from "react";
import { useState } from "react";
import { MDMProvider } from "@/interfaces/MDMProvider";
import {
  EndpointSecurityCheck,
} from "@/interfaces/PostureCheck";
import { PostureCheckCard } from "@/modules/posture-checks/ui/PostureCheckCard";

type Props = {
  value?: EndpointSecurityCheck;
  onChange: (value: EndpointSecurityCheck | undefined) => void;
  disabled?: boolean;
};

export const PostureCheckEndpointSecurity = ({
  value,
  onChange,
  disabled,
}: Props) => {
  const [open, setOpen] = useState(false);

  return (
    <PostureCheckCard
      open={open}
      setOpen={setOpen}
      key={open ? 1 : 0}
      active={!!value?.provider_id}
      title={"Endpoint Security (MDM/EDR)"}
      description={
        "Restrict access in your network based on device compliance reported by an MDM/EDR vendor."
      }
      icon={<ShieldHalf size={16} />}
      iconClass={"bg-gradient-to-tr from-emerald-500 to-emerald-400"}
      modalWidthClass={"max-w-xl"}
      onReset={() => onChange(undefined)}
    >
      <CheckContent
        value={value}
        onChange={(v) => {
          onChange(v);
          setOpen(false);
        }}
        disabled={disabled}
      />
    </PostureCheckCard>
  );
};

const CheckContent = ({ value, onChange, disabled }: Props) => {
  const { data: providers, isLoading } =
    useFetchApi<MDMProvider[]>("/admin/mdm-providers");

  const enabledProviders = (providers || []).filter((p) => p.enabled);

  const [providerId, setProviderId] = useState<string>(
    value?.provider_id ? String(value.provider_id) : "",
  );
  const [failOpen, setFailOpen] = useState<boolean>(value?.fail_open ?? false);

  const options = enabledProviders.map((p) => ({
    label: `${p.name} · ${prettyType(p.type)}`,
    value: String(p.id),
  }));

  const noProviders = !isLoading && enabledProviders.length === 0;

  return (
    <>
      <div className={"flex flex-col px-8 gap-6 pb-6"}>
        <div>
          <Label>MDM/EDR Provider</Label>
          <HelpText>
            Pick a provider configured under Settings → Integrations →
            MDM-EDR. The peer&apos;s hostname is used to look up the device on
            the vendor side.
          </HelpText>
          {noProviders ? (
            <Paragraph className={"text-xs text-nb-gray-300 mt-2"}>
              No MDM/EDR provider is configured. Add one in{" "}
              <InlineLink href={"/settings?tab=integrations"}>
                Settings → Integrations
              </InlineLink>{" "}
              first, then come back to enable this check.
            </Paragraph>
          ) : (
            <SelectDropdown
              value={providerId}
              onChange={setProviderId}
              options={options}
              placeholder={"Select a provider..."}
              showSearch={options.length > 5}
              isLoading={isLoading}
              disabled={disabled}
            />
          )}
        </div>
        <FancyToggleSwitch
          value={failOpen}
          onChange={setFailOpen}
          label={"Fail open on lookup error"}
          helpText={
            "When the vendor lookup itself fails (timeout, vendor outage, device not found), allow the peer through. Default is fail-closed: lookup failure → access denied."
          }
          disabled={disabled}
        />
      </div>
      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <Paragraph className={"text-sm mt-auto"}>
            Learn more about
            <InlineLink
              href={
                "https://docs.openzro.io/how-to/manage-posture-checks#endpoint-security"
              }
              target={"_blank"}
            >
              Endpoint Security
              <ExternalLinkIcon size={12} />
            </InlineLink>
          </Paragraph>
        </div>
        <div className={"flex gap-3 w-full justify-end"}>
          <ModalClose asChild={true}>
            <Button variant={"secondary"}>Cancel</Button>
          </ModalClose>
          <Button
            variant={"primary"}
            disabled={!providerId || disabled}
            onClick={() => {
              const idNum = Number(providerId);
              if (!idNum) {
                onChange(undefined);
              } else {
                onChange({ provider_id: idNum, fail_open: failOpen });
              }
            }}
          >
            Save
          </Button>
        </div>
      </ModalFooter>
    </>
  );
};

const prettyType = (t: string) => {
  switch (t) {
    case "intune":
      return "Microsoft Intune";
    case "sentinelone":
      return "SentinelOne";
    case "huntress":
      return "Huntress";
    default:
      return t;
  }
};
