import { ModalClose, ModalFooter } from "@components/modal/Modal";
import { validator } from "@utils/helpers";
import { isEmpty } from "lodash";
import { ExternalLinkIcon } from "lucide-react";
import * as React from "react";
import { useMemo, useState } from "react";
import OpenzroIcon from "@/assets/icons/OpenzroIcon";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import { OpenzroVersionCheck } from "@/interfaces/PostureCheck";
import { PostureCheckCard } from "@/modules/posture-checks/ui/PostureCheckCard";

type Props = {
  value?: OpenzroVersionCheck;
  onChange: (value: OpenzroVersionCheck | undefined) => void;
  disabled?: boolean;
};

export const PostureCheckOpenzroVersion = ({
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
      active={value?.min_version !== undefined}
      title={"Openzro Client Version"}
      description={
        "Restrict access to peers with a specific Openzro client version."
      }
      icon={<OpenzroIcon size={18} />}
      modalWidthClass={"max-w-lg"}
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
  const [version, setVersion] = useState(value?.min_version || "");

  const versionError = useMemo(() => {
    if (version == "") return "";
    const validSemver = validator.isValidVersion(version);
    if (!validSemver)
      return "Please enter a valid version, e.g., 0.2, 0.2.0, 0.2.0-alpha.1";
  }, [version]);

  const canSave = useMemo(() => {
    return (
      !versionError &&
      version !== value?.min_version &&
      !isEmpty(version) &&
      !disabled
    );
  }, [version, versionError, value, disabled]);

  return (
    <>
      <div className={"flex flex-col px-8 gap-3 pb-6"}>
        <div>
          <OzLabel htmlFor="posture-version">Minimum required version</OzLabel>
          <OzHelpText className="mb-2">
            Only peers with the minimum specified Openzro client version will
            have access to the network.
          </OzHelpText>
          <div>
            <OzInput
              id="posture-version"
              wrapperClassName="max-w-[200px]"
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              placeholder={"e.g., 0.25.0"}
              error={versionError}
              prefix={
                <span className="text-[12.5px] text-oz2-text-faint">
                  Version
                </span>
              }
              disabled={disabled}
            />
          </div>
        </div>
      </div>
      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={
                "https://docs.openzro.io/how-to/manage-posture-checks#net-bird-client-version-check"
              }
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Client Version Check
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
            disabled={!canSave}
            onClick={() => {
              if (isEmpty(version)) {
                onChange(undefined);
              } else {
                onChange({ min_version: version });
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
