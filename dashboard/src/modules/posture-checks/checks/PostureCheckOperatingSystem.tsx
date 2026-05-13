import FancyToggleSwitch from "@components/FancyToggleSwitch";
import { ModalClose, ModalFooter } from "@components/modal/Modal";
import { SelectOption } from "@components/select/SelectDropdown";
import { IconMathEqualGreater } from "@tabler/icons-react";
import { validator } from "@utils/helpers";
import { isEmpty } from "lodash";
import {
  Disc3Icon,
  ExternalLinkIcon,
  FileCog,
  GalleryHorizontalEnd,
  ShieldCheck,
  ShieldXIcon,
} from "lucide-react";
import * as React from "react";
import { useEffect, useMemo, useState } from "react";
import AndroidIcon from "@/assets/icons/AndroidIcon";
import AppleIcon from "@/assets/icons/AppleIcon";
import IOSIcon from "@/assets/icons/IOSIcon";
import { LinuxIcon } from "@/assets/icons/LinuxIcon";
import WindowsIcon from "@/assets/icons/WindowsIcon";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import {
  OzSelect,
  OzSelectContent,
  OzSelectItem,
  OzSelectTrigger,
  OzSelectValue,
} from "@/components/v2/OzSelect";
import {
  OzTabs,
  OzTabsContent,
  OzTabsList,
  OzTabsTrigger,
} from "@/components/v2/OzTabs";
import { OperatingSystem } from "@/interfaces/OperatingSystem";
import {
  androidVersions,
  iOSVersions,
  macOSVersions,
  OperatingSystemVersionCheck,
  windowsKernelVersions,
} from "@/interfaces/PostureCheck";
import { PostureCheckCard } from "@/modules/posture-checks/ui/PostureCheckCard";

type Props = {
  value?: OperatingSystemVersionCheck;
  onChange: (value: OperatingSystemVersionCheck | undefined) => void;
  disabled?: boolean;
};

export const PostureCheckOperatingSystem = ({
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
      icon={<Disc3Icon size={16} />}
      title={"Operating System"}
      modalWidthClass={"max-w-xl"}
      description={
        "Restrict access in your network based on the operating system."
      }
      iconClass={"bg-oz2-bg-sunken text-oz2-text-2"}
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

// OzTabsTrigger className that exposes a named group so the inner
// OS icon can swap fill on the active tab via group-data syntax.
const OS_TAB_TRIGGER = "group/trigger";
const OS_ICON_CLASS =
  "fill-oz2-text-faint group-data-[state=active]/trigger:fill-oz2-acc transition-all";

const CheckContent = ({ value, onChange, disabled }: Props) => {
  const [tab] = useState(String(OperatingSystem.LINUX));

  const firstTimeCheck = value === undefined;

  const [windowsVersion, setWindowsVersion] = useState<string>(
    firstTimeCheck
      ? ""
      : value && value.windows
      ? value.windows.min_kernel_version
      : "-",
  );
  const [macOSVersion, setMacOSVersion] = useState<string>(
    firstTimeCheck
      ? ""
      : value && value.darwin
      ? value.darwin?.min_version
      : "-",
  );
  const [androidVersion, setAndroidVersion] = useState<string>(
    firstTimeCheck
      ? ""
      : value && value.android
      ? value.android?.min_version
      : "-",
  );
  const [iOSVersion, setIOSVersion] = useState<string>(
    firstTimeCheck ? "" : value && value.ios ? value.ios?.min_version : "-",
  );
  const [linuxVersion, setLinuxVersion] = useState<string>(
    firstTimeCheck
      ? ""
      : value && value.linux
      ? value.linux?.min_kernel_version
      : "-",
  );

  const [linuxError, setLinuxError] = useState("");
  const [windowsError, setWindowsError] = useState("");
  const [macOSError, setMacOSError] = useState("");
  const [iOSError, setIOSError] = useState("");
  const [androidError, setAndroidError] = useState("");

  const versionError =
    linuxError ||
    windowsError ||
    macOSError ||
    iOSError ||
    androidError ||
    disabled;

  return (
    <>
      <OzTabs defaultValue={tab}>
        <div className="px-8">
          <OzTabsList>
            <OzTabsTrigger
              value={String(OperatingSystem.LINUX)}
              className={OS_TAB_TRIGGER}
            >
              <LinuxIcon className={OS_ICON_CLASS} />
              Linux
            </OzTabsTrigger>
            <OzTabsTrigger
              value={String(OperatingSystem.WINDOWS)}
              className={OS_TAB_TRIGGER}
            >
              <WindowsIcon className={OS_ICON_CLASS} />
              Windows
            </OzTabsTrigger>
            <OzTabsTrigger
              value={String(OperatingSystem.APPLE)}
              className={OS_TAB_TRIGGER}
            >
              <AppleIcon className={OS_ICON_CLASS} />
              macOS
            </OzTabsTrigger>
            <OzTabsTrigger
              value={String(OperatingSystem.IOS)}
              className={OS_TAB_TRIGGER}
            >
              <IOSIcon className={OS_ICON_CLASS} />
              iOS
            </OzTabsTrigger>
            <OzTabsTrigger
              value={String(OperatingSystem.ANDROID)}
              className={OS_TAB_TRIGGER}
            >
              <AndroidIcon className={OS_ICON_CLASS} />
              Android
            </OzTabsTrigger>
          </OzTabsList>
        </div>
        <OzTabsContent value={String(OperatingSystem.LINUX)} className={"px-8 pt-3"}>
          <OperatingSystemTab
            value={linuxVersion}
            onChange={setLinuxVersion}
            os={OperatingSystem.LINUX}
            onError={setLinuxError}
            disabled={disabled}
          />
        </OzTabsContent>
        <OzTabsContent value={String(OperatingSystem.WINDOWS)} className={"px-8 pt-3"}>
          <OperatingSystemTab
            versionList={windowsKernelVersions}
            value={windowsVersion}
            onChange={setWindowsVersion}
            os={OperatingSystem.WINDOWS}
            onError={setWindowsError}
            disabled={disabled}
          />
        </OzTabsContent>
        <OzTabsContent value={String(OperatingSystem.APPLE)} className={"px-8 pt-3"}>
          <OperatingSystemTab
            versionList={macOSVersions}
            value={macOSVersion}
            onChange={setMacOSVersion}
            os={OperatingSystem.APPLE}
            onError={setMacOSError}
            disabled={disabled}
          />
        </OzTabsContent>
        <OzTabsContent value={String(OperatingSystem.IOS)} className={"px-8 pt-3"}>
          <OperatingSystemTab
            versionList={iOSVersions}
            value={iOSVersion}
            onChange={setIOSVersion}
            os={OperatingSystem.IOS}
            onError={setIOSError}
            disabled={disabled}
          />
        </OzTabsContent>
        <OzTabsContent value={String(OperatingSystem.ANDROID)} className={"px-8 pt-3"}>
          <OperatingSystemTab
            versionList={androidVersions}
            value={androidVersion}
            onChange={setAndroidVersion}
            os={OperatingSystem.ANDROID}
            onError={setAndroidError}
            disabled={disabled}
          />
        </OzTabsContent>
      </OzTabs>
      <div className={"h-6"}></div>
      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={
                "https://docs.openzro.io/how-to/manage-posture-checks#operating-system-version-check"
              }
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Operating System Check
              <ExternalLinkIcon size={12} />
            </a>
          </p>
        </div>
        <div className={"flex gap-3 w-full justify-end"}>
          <ModalClose asChild={true}>
            <OzButton variant={"default"}>Cancel</OzButton>
          </ModalClose>
          <OzButton
            disabled={!!versionError}
            variant={"primary"}
            onClick={() => {
              const osCheck = {} as OperatingSystemVersionCheck;

              if (windowsVersion !== "-") {
                osCheck.windows = { min_kernel_version: windowsVersion };
              }
              if (macOSVersion !== "-") {
                osCheck.darwin = { min_version: macOSVersion };
              }
              if (androidVersion !== "-") {
                osCheck.android = { min_version: androidVersion };
              }
              if (iOSVersion !== "-") {
                osCheck.ios = { min_version: iOSVersion };
              }
              if (linuxVersion !== "-") {
                osCheck.linux = { min_kernel_version: linuxVersion };
              }

              if (isEmpty(osCheck)) {
                onChange(undefined);
              } else {
                onChange(osCheck);
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

type OperatingSystemTabProps = {
  value: string;
  onChange: (value: string) => void;
  versionList?: SelectOption[];
  os: OperatingSystem;
  onError: (error: string) => void;
  disabled?: boolean;
};

const allOrMinOptions = [
  {
    label: "All versions",
    value: "all",
    icon: GalleryHorizontalEnd,
  },
  {
    label: "Equal or greater than",
    value: "min",
    icon: IconMathEqualGreater,
  },
] as SelectOption[];

export const OperatingSystemTab = ({
  value,
  onChange,
  versionList,
  os,
  onError,
  disabled,
}: OperatingSystemTabProps) => {
  const [allow, setAllow] = useState(value == "-" ? "block" : "allow");
  const [allOrMin, setAllOrMin] = useState(
    value == "" || value == "-" || value == "0" ? "all" : "min",
  );
  const [useCustomVersion, setUseCustomVersion] = useState(() => {
    if (!versionList) return false;
    if (!value) return false;
    if (value === "-") return false;
    if (value === "0") return false;
    const find = versionList.map((v) => v.value).includes(value);
    return !find;
  });

  const changeAllow = (value: string) => {
    setAllow(value);
    if (value === "block") {
      setAllOrMin("all");
      onChange("-");
      setAllOrMin("all");
      setUseCustomVersion(false);
    } else {
      onChange("");
      setAllOrMin("all");
      setUseCustomVersion(false);
    }
  };

  const changeAllOrMin = (option: string) => {
    setAllOrMin(option);
    if (option === "all") {
      onChange("");
    } else if (option === "min" && value == "" && versionList) {
      const getLast = versionList[versionList.length - 1];
      onChange(getLast.value);
    }
  };

  const prefix =
    os === OperatingSystem.LINUX || os === OperatingSystem.WINDOWS
      ? "Kernel Version"
      : "Version";

  const versionError = useMemo(() => {
    const msg = "Please enter a valid version, e.g., 0.2, 0.2.0, 0.2.0-alpha.1";
    if (value == "") return "";
    if (value == "-") return "";
    const validSemver = validator.isValidVersion(value);
    if (!validSemver) return msg;
    return "";
  }, [value]);

  useEffect(() => {
    onError(versionError);
  }, [versionError, onError]);

  return (
    <div>
      <div className={"flex justify-between items-start gap-10 "}>
        <div>
          <OzLabel>Allow or Block</OzLabel>
          <OzHelpText className="mt-1">
            Choose whether you want to allow or block the operating system.
          </OzHelpText>
        </div>
        <OzTabs value={allow} onValueChange={changeAllow}>
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
              value={"block"}
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
      <div className={"gap-4 items-center grid grid-cols-2 mt-3"}>
        <OzSelect
          value={allOrMin}
          onValueChange={changeAllOrMin}
          disabled={allow === "block" || disabled}
        >
          <OzSelectTrigger>
            <OzSelectValue />
          </OzSelectTrigger>
          <OzSelectContent>
            {allOrMinOptions.map((opt) => (
              <OzSelectItem key={opt.value} value={opt.value}>
                <span className={"inline-flex items-center gap-2"}>
                  {opt.icon && <opt.icon size={14} width={14} />}
                  {opt.label}
                </span>
              </OzSelectItem>
            ))}
          </OzSelectContent>
        </OzSelect>
        {versionList && !useCustomVersion ? (
          <OzSelect
            value={
              value && value !== "0" && value !== "-" ? value : undefined
            }
            onValueChange={onChange}
            disabled={allOrMin === "all" || allow === "block" || disabled}
          >
            <OzSelectTrigger>
              <OzSelectValue placeholder={"Select version..."} />
            </OzSelectTrigger>
            <OzSelectContent>
              {versionList.map((opt) => (
                <OzSelectItem key={opt.value} value={opt.value}>
                  {opt.label}
                </OzSelectItem>
              ))}
            </OzSelectContent>
          </OzSelect>
        ) : (
          <OzInput
            value={value}
            prefix={
              <span className="text-[12.5px] text-oz2-text-faint">
                {prefix}
              </span>
            }
            placeholder={"e.g., 6.0.0"}
            error={versionError}
            disabled={allOrMin === "all" || allow === "block" || disabled}
            onChange={(v) => {
              onChange(v.target.value);
            }}
          />
        )}
      </div>
      {os !== OperatingSystem.LINUX && (
        <div className={"mt-4"}>
          <FancyToggleSwitch
            disabled={allow === "block" || allOrMin === "all" || disabled}
            value={useCustomVersion}
            onChange={setUseCustomVersion}
            label={
              <>
                <FileCog size={14} />
                Use custom version number
              </>
            }
            helpText={"Use a custom version number if you need more control."}
          />
        </div>
      )}
    </div>
  );
};
