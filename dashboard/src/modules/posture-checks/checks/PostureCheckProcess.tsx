import { ModalClose, ModalFooter } from "@components/modal/Modal";
import { cn, validator } from "@utils/helpers";
import { isEmpty, uniqueId } from "lodash";
import {
  ExternalLinkIcon,
  MinusCircleIcon,
  PlusCircle,
  ServerCogIcon,
  TerminalIcon,
} from "lucide-react";
import * as React from "react";
import { useMemo, useState } from "react";
import AppleIcon from "@/assets/icons/AppleIcon";
import WindowsIcon from "@/assets/icons/WindowsIcon";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import { Process, ProcessCheck } from "@/interfaces/PostureCheck";
import { PostureCheckCard } from "@/modules/posture-checks/ui/PostureCheckCard";

type Props = {
  value?: ProcessCheck;
  onChange: (value: ProcessCheck | undefined) => void;
  disabled?: boolean;
};

export const PostureCheckProcess = ({ value, onChange, disabled }: Props) => {
  const [open, setOpen] = useState(false);

  return (
    <PostureCheckCard
      open={open}
      setOpen={setOpen}
      key={open ? 1 : 0}
      active={value?.processes && value?.processes?.length > 0}
      title={"Process"}
      description={
        "Restrict access in your network based on running processes of a peer."
      }
      icon={<ServerCogIcon size={18} />}
      iconClass={"bg-oz2-bg-sunken text-oz2-text-2"}
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
  const [processes, setProcesses] = useState<Process[]>(
    value?.processes
      ? value.processes.map((p) => {
          return {
            id: uniqueId("process"),
            linux_path: p?.linux_path || "",
            mac_path: p?.mac_path || "",
            windows_path: p?.windows_path || "",
          };
        })
      : [
          {
            id: uniqueId("process"),
            linux_path: "",
            mac_path: "",
            windows_path: "",
          },
        ],
  );

  const handleProcessChange = (
    id: string,
    linux_path: string,
    mac_path: string,
    windows_path: string,
  ) => {
    const newProcesses = processes.map((p) =>
      p.id === id ? { ...p, linux_path, mac_path, windows_path } : p,
    );
    setProcesses(newProcesses);
  };

  const removeProcess = (id: string) => {
    const newProcesses = processes.filter((p) => p.id !== id);
    setProcesses(newProcesses);
  };

  const addProcess = () => {
    setProcesses([
      ...processes,
      {
        id: uniqueId("process"),
        linux_path: "",
        mac_path: "",
        windows_path: "",
      },
    ]);
  };

  const pathErrors = useMemo(() => {
    if (processes && processes.length > 0) {
      return processes.map((p) => {
        return {
          id: p.id,
          errorMacPath: p?.mac_path
            ? validator.isValidUnixFilePath(p?.mac_path || "")
              ? ""
              : "Please enter a valid macOS file path"
            : "",
          errorLinuxPath: p?.linux_path
            ? validator.isValidUnixFilePath(p?.linux_path || "")
              ? ""
              : "Please enter a valid Unix file path"
            : "",
          errorWindowsPath: p?.windows_path
            ? validator.isValidWindowsFilePath(p?.windows_path || "")
              ? ""
              : "Please enter a valid Windows file path"
            : "",
        };
      });
    } else {
      return [];
    }
  }, [processes]);

  const hasErrorsOrIsEmpty = useMemo(() => {
    if (processes.length === 0) return true;
    const hasOnlyEmptyPaths = processes.some(
      (p) => p.linux_path === "" && p.mac_path === "" && p.windows_path === "",
    );
    const hasPathErrors = pathErrors.some(
      (e) =>
        e.errorLinuxPath !== "" ||
        e.errorMacPath !== "" ||
        e.errorWindowsPath !== "",
    );
    return hasOnlyEmptyPaths || hasPathErrors;
  }, [processes, pathErrors]);

  return (
    <>
      <div className={"flex flex-col px-8 gap-2 pb-6"}>
        <div className={"flex justify-between items-start gap-10 mt-2"}>
          <div>
            <OzLabel>Processes</OzLabel>
            <OzHelpText className="mt-1">
              Add the path of an executable file of the process. You can define
              a path for Linux, macOS and Windows. Peers will only be allowed to
              connect if the process is running on their system.
            </OzHelpText>
          </div>
        </div>
        {processes.length > 0 && (
          <div className={"mb-2 flex flex-col gap-4 w-full "}>
            {processes.map((p) => {
              return (
                <div key={p.id} className={"flex gap-2 items-start"}>
                  <div className={"w-full flex flex-col gap-1.5"}>
                    <OzInput
                      prefix={<TerminalIcon size={16} />}
                      placeholder={"/usr/local/bin/openzro"}
                      value={p.linux_path}
                      error={
                        pathErrors.find((e) => e.id === p.id)?.errorLinuxPath
                      }
                      onChange={(e) =>
                        handleProcessChange(
                          p.id,
                          e.target.value,
                          p?.mac_path || "",
                          p?.windows_path || "",
                        )
                      }
                      disabled={disabled}
                    />
                    <OzInput
                      prefix={
                        <AppleIcon
                          size={16}
                          className={cn(
                            pathErrors.find((e) => e.id === p.id)
                              ?.errorMacPath && "fill-oz2-err",
                          )}
                        />
                      }
                      placeholder={
                        "/Applications/Openzro.app/Contents/MacOS/openzro"
                      }
                      value={p.mac_path}
                      error={
                        pathErrors.find((e) => e.id === p.id)?.errorMacPath
                      }
                      onChange={(e) =>
                        handleProcessChange(
                          p.id,
                          p?.linux_path || "",
                          e.target.value,
                          p?.windows_path || "",
                        )
                      }
                      disabled={disabled}
                    />
                    <OzInput
                      prefix={
                        <WindowsIcon
                          size={16}
                          className={cn(
                            pathErrors.find((e) => e.id === p.id)
                              ?.errorWindowsPath && "fill-oz2-err",
                          )}
                        />
                      }
                      placeholder={`C:\\ProgramData\\Openzro\\openzro.exe`}
                      value={p.windows_path}
                      error={
                        pathErrors.find((e) => e.id === p.id)?.errorWindowsPath
                      }
                      onChange={(e) =>
                        handleProcessChange(
                          p.id,
                          p?.linux_path || "",
                          p?.mac_path || "",
                          e.target.value,
                        )
                      }
                      disabled={disabled}
                    />
                  </div>

                  <button
                    type="button"
                    onClick={() => removeProcess(p.id)}
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
          onClick={addProcess}
          disabled={disabled}
          className="mt-1 inline-flex h-[34px] items-center justify-center gap-2 rounded-oz2-input border border-dashed border-oz2-border-strong bg-transparent px-3 text-[13px] font-medium text-oz2-text-muted transition-colors hover:border-oz2-acc hover:bg-oz2-acc-soft/50 hover:text-oz2-acc-text disabled:cursor-not-allowed disabled:opacity-50"
        >
          <PlusCircle size={16} />
          Add Process
        </button>
      </div>
      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={
                "https://docs.openzro.io/how-to/manage-posture-checks#process-check"
              }
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Process Check
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
              if (isEmpty(processes)) {
                onChange(undefined);
              } else {
                onChange({
                  processes: processes.filter(
                    (p) =>
                      p.linux_path !== "" ||
                      p.mac_path !== "" ||
                      p.windows_path !== "",
                  ),
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
