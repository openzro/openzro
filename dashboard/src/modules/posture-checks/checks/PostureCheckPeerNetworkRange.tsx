import { ModalClose, ModalFooter } from "@components/modal/Modal";
import cidr from "ip-cidr";
import { isEmpty, uniqueId } from "lodash";
import {
  ExternalLinkIcon,
  MinusCircleIcon,
  NetworkIcon,
  PlusCircle,
  ShieldCheck,
  ShieldXIcon,
} from "lucide-react";
import * as React from "react";
import { useMemo, useState } from "react";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import { OzTabs, OzTabsList, OzTabsTrigger } from "@/components/v2/OzTabs";
import { PeerNetworkRangeCheck } from "@/interfaces/PostureCheck";
import { PostureCheckCard } from "@/modules/posture-checks/ui/PostureCheckCard";

type Props = {
  value?: PeerNetworkRangeCheck;
  onChange: (value: PeerNetworkRangeCheck | undefined) => void;
  disabled?: boolean;
};

export const PostureCheckPeerNetworkRange = ({
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
      icon={<NetworkIcon size={16} />}
      title={"Peer Network Range"}
      modalWidthClass={"max-w-xl"}
      description={
        "Restrict access by allowing or blocking peer network ranges."
      }
      iconClass={
        "bg-sky-100 text-sky-700 dark:bg-sky-950/50 dark:text-sky-200"
      }
      active={value !== undefined}
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

interface NetworkRange {
  id: string;
  value: string;
}

const CheckContent = ({ value, onChange, disabled }: Props) => {
  const [allowOrDeny, setAllowOrDeny] = useState<string>(
    value?.action ? value.action : "allow",
  );

  const [networkRanges, setNetworkRanges] = useState<NetworkRange[]>(
    value?.ranges
      ? value.ranges.map((r) => {
          return {
            id: uniqueId("range"),
            value: r,
          };
        })
      : [],
  );

  const handleNetworkRangeChange = (id: string, value: string) => {
    const newRanges = networkRanges.map((r) =>
      r.id === id ? { ...r, value } : r,
    );
    setNetworkRanges(newRanges);
  };

  const removeNetworkRange = (id: string) => {
    const newRanges = networkRanges.filter((r) => r.id !== id);
    setNetworkRanges(newRanges);
  };

  const addNetworkRange = () => {
    setNetworkRanges([...networkRanges, { id: uniqueId("range"), value: "" }]);
  };

  const validateNetworkRange = (networkRange: string) => {
    if (networkRange == "") return "";
    const validCIDR = cidr.isValidAddress(networkRange);
    if (!validCIDR) return "Please enter a valid CIDR, e.g., 192.168.1.0/24";
    return "";
  };

  const cidrErrors = useMemo(() => {
    if (networkRanges && networkRanges.length > 0) {
      return networkRanges.map((r) => {
        return {
          id: r.id,
          error: validateNetworkRange(r.value),
        };
      });
    } else {
      return [];
    }
  }, [networkRanges]);

  const hasErrorsOrIsEmpty = useMemo(() => {
    if (networkRanges.length === 0) return true;
    return cidrErrors.some((e) => e.error !== "");
  }, [networkRanges, cidrErrors]);

  return (
    <>
      <div className={"flex flex-col px-8 gap-2 pb-6"}>
        <div className={"flex justify-between items-start gap-10 mt-2"}>
          <div>
            <OzLabel>Allow or Block Ranges</OzLabel>
            <OzHelpText className="mt-1">
              Choose whether you want to allow or block specific peer network
              ranges
            </OzHelpText>
          </div>
          <OzTabs value={allowOrDeny} onValueChange={setAllowOrDeny}>
            <OzTabsList>
              <OzTabsTrigger
                value={"allow"}
                className={
                  "gap-1.5 " +
                  "data-[state=active]:!bg-emerald-500/15 " +
                  "data-[state=active]:!text-emerald-700 " +
                  "data-[state=active]:!shadow-none " +
                  "dark:data-[state=active]:!text-emerald-300"
                }
              >
                <ShieldCheck size={14} />
                Allow
              </OzTabsTrigger>
              <OzTabsTrigger
                value={"deny"}
                className={
                  "gap-1.5 " +
                  "data-[state=active]:!bg-red-500/15 " +
                  "data-[state=active]:!text-red-700 " +
                  "data-[state=active]:!shadow-none " +
                  "dark:data-[state=active]:!text-red-300"
                }
              >
                <ShieldXIcon size={14} />
                Block
              </OzTabsTrigger>
            </OzTabsList>
          </OzTabs>
        </div>
        {networkRanges.length > 0 && (
          <div className={"mb-2 flex flex-col gap-2 w-full "}>
            {networkRanges.map((ipRange) => {
              return (
                <div key={ipRange.id} className={"flex gap-2 items-start"}>
                  <div className={"w-full"}>
                    <OzInput
                      prefix={<NetworkIcon size={16} />}
                      placeholder={"e.g., 172.16.0.0/16"}
                      value={ipRange.value}
                      error={cidrErrors.find((e) => e.id === ipRange.id)?.error}
                      mono
                      onChange={(e) =>
                        handleNetworkRangeChange(ipRange.id, e.target.value)
                      }
                      disabled={disabled}
                    />
                  </div>

                  <button
                    type="button"
                    onClick={() => removeNetworkRange(ipRange.id)}
                    disabled={disabled}
                    className="grid h-[34px] w-[34px] shrink-0 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:border-oz2-border-strong disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <MinusCircleIcon size={15} />
                  </button>
                </div>
              );
            })}
          </div>
        )}
        <button
          type="button"
          onClick={addNetworkRange}
          disabled={disabled}
          className="inline-flex h-[34px] items-center justify-center gap-2 rounded-oz2-input border border-dashed border-oz2-border-strong bg-transparent px-3 text-[13px] font-medium text-oz2-text-muted transition-colors hover:border-oz2-acc hover:bg-oz2-acc-soft/50 hover:text-oz2-acc-text disabled:cursor-not-allowed disabled:opacity-50"
        >
          <PlusCircle size={16} />
          Add Network Range
        </button>
      </div>
      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={
                "https://docs.openzro.io/how-to/manage-posture-checks#peer-network-range-check"
              }
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Peer Network Range Check
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
            disabled={hasErrorsOrIsEmpty || disabled}
            onClick={() => {
              if (isEmpty(networkRanges)) {
                onChange(undefined);
              } else {
                onChange({
                  action: allowOrDeny as "allow" | "deny",
                  ranges: networkRanges
                    .map((r) => r.value)
                    .filter((r) => r !== ""),
                });
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
