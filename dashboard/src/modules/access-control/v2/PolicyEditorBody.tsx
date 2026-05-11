"use client";

import { Callout } from "@components/Callout";
import FancyToggleSwitch from "@components/FancyToggleSwitch";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { PortSelector } from "@components/PortSelector";
import PolicyDirection from "@components/ui/PolicyDirection";
import { cn } from "@utils/helpers";
import {
  AlertCircleIcon,
  FolderDown,
  FolderInput,
  Power,
  Share2,
  Shield,
  ShieldCheck,
} from "lucide-react";
import * as React from "react";
import { useState } from "react";
import Skeleton from "react-loading-skeleton";
import OzCard from "@/components/v2/OzCard";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import {
  OzSelect,
  OzSelectContent,
  OzSelectItem,
  OzSelectTrigger,
  OzSelectValue,
} from "@/components/v2/OzSelect";
import OzTextarea from "@/components/v2/OzTextarea";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Group } from "@/interfaces/Group";
import { Policy, Protocol } from "@/interfaces/Policy";
import { PostureCheck } from "@/interfaces/PostureCheck";
import { useAccessControl } from "@/modules/access-control/useAccessControl";
import { PostureCheckBrowseModal } from "@/modules/posture-checks/modal/PostureCheckBrowseModal";
import PostureCheckModal from "@/modules/posture-checks/modal/PostureCheckModal";
import PostureCheckMinimalTable from "@/modules/posture-checks/table/PostureCheckMinimalTable";

// PolicyEditorBody — full-page version of the form previously hosted
// in AccessControlModalContent. The 3-tab modal flow (Policy / Posture
// Checks / Name & Description) collapses into 3 stacked OzCards so the
// operator can see the whole policy at once instead of step-walking
// through tabs. The useAccessControl hook owns all state + submit, so
// behavior parity with the modal is preserved verbatim — only the
// chrome and ordering change.
//
// The component does not own the topbar Save/Cancel buttons; the page
// shell does. To submit programmatically, parents drive the editor via
// the ref returned by useImperativeHandle below.

export type PolicyEditorHandle = {
  submit: () => void;
  isSubmitDisabled: () => boolean;
  isDirty: () => boolean;
};

type Props = {
  // Existing policy when editing. Undefined = create.
  policy?: Policy;
  // Pre-seeded groups for the "Create policy for this route" flow
  // (still used by RouteModal via the legacy modal path).
  initialDestinationGroups?: Group[] | string[];
  initialName?: string;
  initialDescription?: string;
  // Optional templates surfaced as pre-selected posture checks.
  postureCheckTemplates?: PostureCheck[];
  // Fires after a successful submit. Page-shell uses this to navigate
  // back to /access-control with a fresh SWR cache.
  onSuccess?: (policy: Policy) => void;
  // When false the form holds the policy in memory and the parent
  // composes it into a larger save (the RouteModal flow). The page
  // editor always submits directly.
  useSave?: boolean;
};

const PolicyEditorBody = React.forwardRef<PolicyEditorHandle, Props>(
  function PolicyEditorBody(
    {
      policy,
      initialDestinationGroups,
      initialName,
      initialDescription,
      postureCheckTemplates,
      onSuccess,
      useSave = true,
    },
    ref,
  ) {
    const { permission } = usePermissions();

    const {
      portDisabled,
      destinationGroups,
      direction,
      ports,
      sourceGroups,
      destinationHasResources,
      destinationOnlyResources,
      setSourceGroups,
      setDestinationGroups,
      setPorts,
      setDirection,
      setProtocol,
      enabled,
      setEnabled,
      setName,
      setDescription,
      setPostureChecks,
      name,
      protocol,
      description,
      postureChecks,
      submit,
      isPostureChecksLoading,
      destinationResource,
      setDestinationResource,
      portRanges,
      setPortRanges,
      hasPortSupport,
    } = useAccessControl({
      policy,
      postureCheckTemplates,
      onSuccess,
      initialDestinationGroups,
      initialName,
      initialDescription,
    });

    const isSubmitDisabled = (): boolean => {
      if (name.length === 0) return true;
      if (sourceGroups.length === 0) return true;
      if (destinationGroups.length === 0 && !destinationResource) return true;
      if (policy && !permission.policies.update) return true;
      if (!policy && !permission.policies.create) return true;
      return false;
    };

    React.useImperativeHandle(
      ref,
      () => ({
        submit: () => {
          if (useSave) submit();
          else onSuccess?.(policy ?? ({} as Policy));
        },
        isSubmitDisabled,
        isDirty: () => name.length > 0 || description.length > 0,
      }),
      // The hook returns stable setters, so we depend on the values
      // that drive isSubmitDisabled.
      // eslint-disable-next-line react-hooks/exhaustive-deps
      [
        name,
        description,
        sourceGroups,
        destinationGroups,
        destinationResource,
        useSave,
      ],
    );

    const handleProtocolChange = (p: Protocol) => {
      setProtocol(p);
      if (!hasPortSupport(p)) {
        setPorts([]);
        setPortRanges([]);
      }
    };

    const fieldsDisabled =
      !permission.policies.update || !permission.policies.create;

    return (
      <div className="flex flex-col gap-4">
        {/* Card 1 — Identity. Mirrors handoff Identity: name + status
            tile side-by-side at the top, description spans the row
            below. Trades the modal's stacked-everything layout for
            the wider horizontal posture the page allows. */}
        <OzCard>
          <div className="grid grid-cols-1 gap-5 md:grid-cols-[minmax(0,1fr)_240px]">
            <div>
              <OzLabel htmlFor="policy-name" required>
                Policy name
              </OzLabel>
              <OzHelpText className="mb-2">
                Set an easily identifiable name for your policy.
              </OzHelpText>
              <OzInput
                id="policy-name"
                autoFocus={!policy}
                tabIndex={0}
                value={name}
                data-cy="policy-name"
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g., Devs to Servers"
                disabled={fieldsDisabled}
              />
            </div>

            {/* Compact enable tile — same FancyToggleSwitch the modal
                used, but constrained to the 240px column so the right
                edge of the card stays aligned with the cards below. */}
            <div className="md:pt-7">
              <FancyToggleSwitch
                value={enabled}
                onChange={setEnabled}
                disabled={fieldsDisabled}
                label={
                  <>
                    <Power size={15} />
                    Enable Policy
                  </>
                }
                helpText="Toggle the policy on or off."
              />
            </div>
          </div>

          <div className="mt-5">
            <OzLabel htmlFor="policy-description" optional>
              Description
            </OzLabel>
            <OzHelpText className="mb-2">
              Write a short description to add more context to this policy.
            </OzHelpText>
            <OzTextarea
              id="policy-description"
              value={description}
              data-cy="policy-description"
              onChange={(e) => setDescription(e.target.value)}
              placeholder="e.g., Devs are allowed to access servers and servers are allowed to access Devs."
              rows={2}
              disabled={fieldsDisabled}
            />
          </div>
        </OzCard>

        {/* Card 2 — Source → Destination. Three columns with the
            PolicyDirection toggle wedged between, matching the handoff
            layout where the direction glyph sits visually on the path
            between the two pickers. Callouts stack below the row. */}
        <OzCard>
          <div className="grid grid-cols-1 items-start gap-6 md:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]">
            <div>
              <OzLabel className="mb-2 inline-flex items-center gap-2">
                <FolderDown size={14} />
                Source
              </OzLabel>
              <PeerGroupSelector
                dataCy="source-group-selector"
                showPeerCount
                disableInlineRemoveGroup={false}
                popoverWidth={500}
                showRoutes={false}
                onChange={setSourceGroups}
                values={sourceGroups}
                saveGroupAssignments={useSave}
                showResourceCounter={false}
                disabled={fieldsDisabled}
              />
            </div>

            <div className="hidden self-center md:block">
              <PolicyDirection
                value={direction}
                onChange={setDirection}
                disabled={destinationOnlyResources}
                destinationResource={destinationResource}
              />
            </div>
            {/* Mobile: PolicyDirection collapses below the source picker
                instead of disappearing on narrow viewports. */}
            <div className="md:hidden">
              <PolicyDirection
                value={direction}
                onChange={setDirection}
                disabled={destinationOnlyResources}
                destinationResource={destinationResource}
              />
            </div>

            <div>
              <OzLabel className="mb-2 inline-flex items-center gap-2">
                <FolderInput size={14} />
                Destination
              </OzLabel>
              <PeerGroupSelector
                dataCy="destination-group-selector"
                showRoutes
                showPeerCount
                disableInlineRemoveGroup={false}
                popoverWidth={500}
                onChange={setDestinationGroups}
                values={destinationGroups}
                saveGroupAssignments={useSave}
                resource={destinationResource}
                onResourceChange={setDestinationResource}
                showResources
                placeholder="Select destination(s)..."
                disabled={fieldsDisabled}
              />
            </div>
          </div>

          {destinationHasResources &&
            !destinationOnlyResources &&
            direction === "bi" && (
              <Callout
                variant="warning"
                icon={
                  <AlertCircleIcon
                    size={14}
                    className="shrink-0 relative top-[3px] text-oz2-acc"
                  />
                }
                className="mt-5"
              >
                Some destination groups contain resources. Resources only
                support incoming traffic and cannot initiate connections.
              </Callout>
            )}

          {protocol === "all" && direction !== "bi" && (
            <Callout
              variant="warning"
              icon={
                <AlertCircleIcon
                  size={14}
                  className="shrink-0 relative top-[3px] text-oz2-acc"
                />
              }
              className="mt-5"
              data-cy="unidirectional-all-warning"
            >
              Unidirectional ALL is experimental. Reply traffic relies on
              the firewall&apos;s stateful conntrack — fine for
              request/response protocols (HTTP, SSH, DNS), but apps that
              push unsolicited messages from the destination back to the
              source (SNMP traps, syslog UDP outbound, server-initiated
              heartbeats) will be dropped. Operationally-asymmetric ICMP
              (destination-unreachable, fragmentation-needed) is dropped
              too. Keep ALL bidirectional or split into per-protocol rules
              if your apps depend on those.{" "}
              <a
                className="underline"
                href="https://github.com/openzro/openzro/blob/main/docs/operator/unidirectional-policies.md"
                target="_blank"
                rel="noreferrer noopener"
              >
                Operator guide
              </a>
              .
            </Callout>
          )}
        </OzCard>

        {/* Card 3 — Protocol & Ports. Protocol selector + label/help
            block sit on a single horizontal row at the top, Ports
            sits below a soft divider. Pulls these out of the
            Source/Destination card so each card has a single clear
            responsibility per the handoff "Ports & protocols" group. */}
        <OzCard>
          <div
            className="flex flex-wrap items-start justify-between gap-4"
            data-cy="protocol-wrapper"
          >
            <div className="min-w-0 flex-1">
              <OzLabel className="inline-flex items-center gap-2">
                <Share2 size={14} />
                Protocol
              </OzLabel>
              <OzHelpText className="mt-1 max-w-md">
                Allow only specified network protocols. Select{" "}
                <b className="text-oz2-text">TCP</b> or{" "}
                <b className="text-oz2-text">UDP</b> to constrain ports.
              </OzHelpText>
            </div>
            <div className="w-[140px] shrink-0">
              <OzSelect
                value={protocol}
                onValueChange={(v) => handleProtocolChange(v as Protocol)}
                disabled={fieldsDisabled}
              >
                <OzSelectTrigger>
                  <div
                    className="flex items-center gap-2"
                    data-cy="protocol-select-button"
                  >
                    <OzSelectValue placeholder="Protocol" />
                  </div>
                </OzSelectTrigger>
                <OzSelectContent data-cy="protocol-selection">
                  <OzSelectItem value="all">ALL</OzSelectItem>
                  <OzSelectItem value="tcp">TCP</OzSelectItem>
                  <OzSelectItem value="udp">UDP</OzSelectItem>
                  <OzSelectItem value="icmp">ICMP</OzSelectItem>
                </OzSelectContent>
              </OzSelect>
            </div>
          </div>

          <div className="my-5 h-px bg-oz2-border-soft" aria-hidden />

          <div
            className={cn(portDisabled && "opacity-30 pointer-events-none")}
          >
            <OzLabel className="inline-flex items-center gap-2">
              <Shield size={14} />
              Ports
            </OzLabel>
            <OzHelpText className="mt-1 mb-2">
              Allow access only to specified ports. Pick individual ports
              or ranges between 1 and 65535.
            </OzHelpText>
            <PortSelector
              showAll
              ports={ports}
              onPortsChange={setPorts}
              portRanges={portRanges}
              onPortRangesChange={setPortRanges}
              disabled={portDisabled}
            />
          </div>
        </OzCard>

        {/* Card 4 — Posture Checks. Inlined from the legacy
            PostureCheckTab so the page doesn't need a tab wrapper to
            host the same minimal table + Add/Browse modals. */}
        <PostureCheckCard
          postureChecks={postureChecks}
          setPostureChecks={setPostureChecks}
          isLoading={isPostureChecksLoading}
        />
      </div>
    );
  },
);

export default PolicyEditorBody;

// PostureCheckCard wraps the same minimal-table + Add/Browse-modal
// composition the legacy PostureCheckTab used, swapping the
// OzTabsContent wrapper for an OzCard so it can sit alongside the
// other policy cards in the page editor.
function PostureCheckCard({
  postureChecks,
  setPostureChecks,
  isLoading,
}: {
  postureChecks: PostureCheck[];
  setPostureChecks: React.Dispatch<React.SetStateAction<PostureCheck[]>>;
  isLoading: boolean;
}) {
  const addPostureChecks = (checks: PostureCheck[]) => {
    setPostureChecks((prev) => {
      const previous = prev.map((check) => {
        const find = checks.find((c) => c.id === check.id);
        if (find) return find;
        return check;
      });
      const allChecks = [...previous, ...checks];
      return allChecks.filter(
        (check, index, self) =>
          self.findIndex((c) => c.id === check.id) === index,
      );
    });
  };

  const removePostureCheck = (check: PostureCheck) => {
    setPostureChecks((prev) => prev.filter((c) => c.id !== check.id));
  };

  const [checkModal, setCheckModal] = useState(false);
  const [browseModal, setBrowseModal] = useState(false);
  const [currentEditCheck, setCurrentEditCheck] = useState<PostureCheck>();

  return (
    <OzCard>
      <div className="mb-4 flex items-start gap-3">
        <span className="grid h-8 w-8 shrink-0 place-items-center rounded-[8px] bg-oz2-acc-soft text-oz2-acc-text">
          <ShieldCheck size={14} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="text-[14px] font-semibold text-oz2-text">
            Posture Checks
          </div>
          <p className="mt-0.5 text-[12.5px] leading-[1.5] text-oz2-text-muted">
            Conditions a source peer must satisfy before this policy
            applies — OS, agent version, geofencing. Empty list means the
            policy applies unconditionally.
          </p>
        </div>
      </div>

      {isLoading ? (
        <div className="flex flex-col gap-2">
          <Skeleton width="100%" height={41} />
          <Skeleton width="100%" height={42} />
          <Skeleton width="100%" height={42} />
          <Skeleton width="100%" height={41} />
        </div>
      ) : (
        <>
          {checkModal && (
            <PostureCheckModal
              open={checkModal}
              onOpenChange={setCheckModal}
              onSuccess={(check) => {
                addPostureChecks([check]);
                setCheckModal(false);
              }}
              postureCheck={currentEditCheck}
            />
          )}
          {browseModal && (
            <PostureCheckBrowseModal
              open={browseModal}
              onOpenChange={setBrowseModal}
              onSuccess={(check) => addPostureChecks(check)}
            />
          )}
          <PostureCheckMinimalTable
            data={postureChecks}
            onEditClick={(check) => {
              setCurrentEditCheck(check);
              setCheckModal(true);
            }}
            onAddClick={() => {
              setCurrentEditCheck(undefined);
              setCheckModal(true);
            }}
            onBrowseClick={() => {
              setCurrentEditCheck(undefined);
              setBrowseModal(true);
            }}
            onRemoveClick={removePostureCheck}
          />
        </>
      )}
    </OzCard>
  );
}

