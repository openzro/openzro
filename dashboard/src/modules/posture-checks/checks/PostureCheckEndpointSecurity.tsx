import FancyToggleSwitch from "@components/FancyToggleSwitch";
import { ModalClose, ModalFooter } from "@components/modal/Modal";
import { SelectDropdown } from "@components/select/SelectDropdown";
import useFetchApi from "@utils/api";
import { ExternalLinkIcon, ShieldHalf } from "lucide-react";
import Link from "next/link";
import * as React from "react";
import { useState } from "react";
import OzButton from "@/components/v2/OzButton";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import { MDMProvider } from "@/interfaces/MDMProvider";
import { EndpointSecurityCheck } from "@/interfaces/PostureCheck";
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
          <OzLabel>MDM/EDR Provider</OzLabel>
          <OzHelpText className="mb-2">
            Pick a provider configured under Settings → Integrations →
            MDM-EDR. The peer&apos;s hostname is used to look up the device on
            the vendor side.
          </OzHelpText>
          {noProviders ? (
            <p className={"text-xs text-oz2-text-muted mt-2"}>
              No MDM/EDR provider is configured. Add one in{" "}
              <Link
                href={"/settings?tab=integrations"}
                className="text-oz2-acc-text underline-offset-2 hover:underline"
              >
                Settings → Integrations
              </Link>{" "}
              first, then come back to enable this check.
            </p>
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
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={
                "https://docs.openzro.io/how-to/manage-posture-checks#endpoint-security"
              }
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Endpoint Security
              <ExternalLinkIcon size={12} />
            </a>
          </p>
        </div>
        <div className={"flex gap-3 w-full justify-end"}>
          <ModalClose asChild={true}>
            <OzButton variant={"default"}>Cancel</OzButton>
          </ModalClose>
          <OzButton
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
          </OzButton>
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
