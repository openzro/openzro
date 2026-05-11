import { Modal, ModalContent, ModalFooter } from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { cn } from "@utils/helpers";
import { isEmpty } from "lodash";
import { ExternalLinkIcon, LayoutList, ShieldCheck, Text } from "lucide-react";
import React, { useState } from "react";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import {
  OzTabs,
  OzTabsContent,
  OzTabsList,
  OzTabsTrigger,
} from "@/components/v2/OzTabs";
import OzTextarea from "@/components/v2/OzTextarea";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { PostureCheck } from "@/interfaces/PostureCheck";
import { PostureCheckEndpointSecurity } from "@/modules/posture-checks/checks/PostureCheckEndpointSecurity";
import { PostureCheckGeoLocation } from "@/modules/posture-checks/checks/PostureCheckGeoLocation";
import { PostureCheckOpenzroVersion } from "@/modules/posture-checks/checks/PostureCheckOpenzroVersion";
import { PostureCheckOperatingSystem } from "@/modules/posture-checks/checks/PostureCheckOperatingSystem";
import { PostureCheckPeerNetworkRange } from "@/modules/posture-checks/checks/PostureCheckPeerNetworkRange";
import { PostureCheckProcess } from "@/modules/posture-checks/checks/PostureCheckProcess";
import { usePostureCheck } from "@/modules/posture-checks/usePostureCheck";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess?: (check: PostureCheck) => void;
  postureCheck?: PostureCheck;
  useSave?: boolean;
};

export default function PostureCheckModal({
  open,
  onOpenChange,
  onSuccess,
  postureCheck,
  useSave = true,
}: Props) {
  const { permission } = usePermissions();

  const {
    state: check,
    dispatch: setCheck,
    updateOrCreateAndNotify: updateOrCreate,
  } = usePostureCheck({
    postureCheck,
    onSuccess,
  });

  const close = () => {
    onSuccess && onSuccess(check);
  };

  const isAtLeastOneCheckEnabled =
    !!check?.checks?.nb_version_check ||
    !!check?.checks?.geo_location_check ||
    !!check?.checks?.os_version_check ||
    !!check?.checks?.peer_network_range_check ||
    !!check?.checks?.process_check ||
    !!check?.checks?.endpoint_security_check;
  const canCreate =
    !isEmpty(check?.name) &&
    isAtLeastOneCheckEnabled &&
    (permission.policies.create || permission.policies.update);

  const [tab, setTab] = useState("checks");

  return (
    <>
      <Modal open={open} onOpenChange={onOpenChange} key={open ? 1 : 0}>
        <ModalContent
          maxWidthClass={cn("relative", "max-w-2xl")}
          showClose={true}
        >
          <ModalHeader
            icon={<ShieldCheck size={19} />}
            title={
              postureCheck ? "Update Posture Check" : "Create Posture Check"
            }
            description={
              "Use posture checks to further restrict access in your network."
            }
            color={"openzro"}
          />

          <OzTabs onValueChange={(v) => setTab(v)} defaultValue={tab} value={tab}>
            <div className="px-8">
              <OzTabsList>
                <OzTabsTrigger value={"checks"}>
                  <LayoutList size={16} />
                  Checks
                </OzTabsTrigger>

                <OzTabsTrigger
                  value={"general"}
                  disabled={!isAtLeastOneCheckEnabled}
                >
                  <Text size={16} />
                  Name & Description
                </OzTabsTrigger>
              </OzTabsList>
            </div>

            <OzTabsContent value={"checks"} className={"pb-6 px-8"}>
              <>
                <PostureCheckOpenzroVersion
                  value={check?.checks?.nb_version_check}
                  onChange={(v) =>
                    setCheck({
                      type: "version",
                      payload: v,
                    })
                  }
                  disabled={
                    !permission.policies.create || !permission.policies.update
                  }
                />
                <PostureCheckGeoLocation
                  value={check?.checks?.geo_location_check}
                  onChange={(v) =>
                    setCheck({
                      type: "location",
                      payload: v,
                    })
                  }
                  disabled={
                    !permission.policies.create || !permission.policies.update
                  }
                />
                <PostureCheckPeerNetworkRange
                  value={check?.checks?.peer_network_range_check}
                  onChange={(v) =>
                    setCheck({
                      type: "network_range",
                      payload: v,
                    })
                  }
                  disabled={
                    !permission.policies.create || !permission.policies.update
                  }
                />
                <PostureCheckOperatingSystem
                  value={check?.checks?.os_version_check}
                  onChange={(v) =>
                    setCheck({
                      type: "os",
                      payload: v,
                    })
                  }
                  disabled={
                    !permission.policies.create || !permission.policies.update
                  }
                />
                <PostureCheckProcess
                  value={check?.checks?.process_check}
                  onChange={(v) =>
                    setCheck({
                      type: "process_check",
                      payload: v,
                    })
                  }
                  disabled={
                    !permission.policies.create || !permission.policies.update
                  }
                />
                <PostureCheckEndpointSecurity
                  value={check?.checks?.endpoint_security_check}
                  onChange={(v) =>
                    setCheck({
                      type: "endpoint_security",
                      payload: v,
                    })
                  }
                  disabled={
                    !permission.policies.create || !permission.policies.update
                  }
                />
              </>
            </OzTabsContent>
            <OzTabsContent value={"general"} className={"pb-8 px-8"}>
              <div className={"flex flex-col gap-6 pt-2"}>
                <div>
                  <OzLabel htmlFor="posture-name">
                    Name of the Posture Check
                  </OzLabel>
                  <OzHelpText className="mb-2">
                    Set an easily identifiable name for your posture check.
                  </OzHelpText>
                  <OzInput
                    id="posture-name"
                    autoFocus={true}
                    tabIndex={0}
                    value={check?.name}
                    onChange={(e) =>
                      setCheck({
                        type: "name",
                        payload: e.target.value,
                      })
                    }
                    placeholder={"e.g., Openzro Version > 0.25.0"}
                    disabled={
                      !permission.policies.create || !permission.policies.update
                    }
                  />
                </div>
                <div>
                  <OzLabel htmlFor="posture-description" optional>
                    Description
                  </OzLabel>
                  <OzHelpText className="mb-2">
                    Write a short description to add more context to this
                    policy.
                  </OzHelpText>
                  <OzTextarea
                    id="posture-description"
                    value={check?.description}
                    onChange={(e) =>
                      setCheck({
                        type: "description",
                        payload: e.target.value,
                      })
                    }
                    placeholder={
                      "e.g., Check if the Openzro version is bigger than 0.25.0"
                    }
                    rows={3}
                    disabled={
                      !permission.policies.create || !permission.policies.update
                    }
                  />
                </div>
              </div>
            </OzTabsContent>
          </OzTabs>

          <ModalFooter className={"items-center"}>
            <div className={"w-full"}>
              <p className={"text-sm mt-auto text-oz2-text-muted"}>
                Learn more about{" "}
                <a
                  href={"https://docs.openzro.io/how-to/manage-posture-checks"}
                  target={"_blank"}
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
                >
                  Posture Checks
                  <ExternalLinkIcon size={12} />
                </a>
              </p>
            </div>
            <div className={"flex gap-3 w-full justify-end"}>
              <>
                {tab == "checks" && (
                  <OzButton
                    variant={"default"}
                    onClick={() => onOpenChange(false)}
                  >
                    Cancel
                  </OzButton>
                )}

                {tab == "general" && (
                  <OzButton
                    variant={"default"}
                    onClick={() => setTab("checks")}
                  >
                    Back
                  </OzButton>
                )}

                {!postureCheck && tab == "checks" && (
                  <OzButton
                    variant={"primary"}
                    onClick={() => setTab("general")}
                    disabled={!isAtLeastOneCheckEnabled}
                  >
                    Continue
                  </OzButton>
                )}

                {((!postureCheck && tab == "general") || postureCheck) && (
                  <OzButton
                    variant={"primary"}
                    disabled={!canCreate}
                    onClick={() => {
                      if (useSave) {
                        updateOrCreate();
                      } else {
                        close();
                      }
                    }}
                  >
                    {postureCheck ? "Save Changes" : "Create Posture Check"}
                  </OzButton>
                )}
              </>
            </div>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </>
  );
}
