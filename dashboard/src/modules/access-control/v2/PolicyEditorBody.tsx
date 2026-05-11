"use client";

import { Callout } from "@components/Callout";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import { PortSelector } from "@components/PortSelector";
import PolicyDirection from "@components/ui/PolicyDirection";
import * as LabelPrimitive from "@radix-ui/react-label";
import { cn } from "@utils/helpers";
import {
  AlertCircleIcon,
  ArrowRightLeft,
  Shield,
  ShieldCheck,
} from "lucide-react";
import * as React from "react";
import { useState } from "react";
import Skeleton from "react-loading-skeleton";
import OzCard from "@/components/v2/OzCard";
import OzInput from "@/components/v2/OzInput";
import { OzHelpText } from "@/components/v2/OzLabel";
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
import OzTextarea from "@/components/v2/OzTextarea";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Group } from "@/interfaces/Group";
import { Policy, Protocol } from "@/interfaces/Policy";
import { PostureCheck } from "@/interfaces/PostureCheck";
import { useAccessControl } from "@/modules/access-control/useAccessControl";
import PolicyImpact from "@/modules/access-control/v2/PolicyImpact";
import PolicyLivePreview from "@/modules/access-control/v2/PolicyLivePreview";
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

    // No deps array — the factory runs every render so the closures
    // it captures (submit, isSubmitDisabled) always see the latest
    // form state. The earlier version listed only a subset of inputs
    // (name, description, source/destination groups) which left
    // ports / port ranges / protocol / enabled / direction / posture
    // checks behind: changing those after the first render kept the
    // shell calling a stale `submit` that persisted the old values.
    // Re-running each render costs only a function-object allocation
    // and is what useImperativeHandle is designed for.
    React.useImperativeHandle(ref, () => ({
      submit: () => {
        if (useSave) submit();
        else onSuccess?.(policy ?? ({} as Policy));
      },
      isSubmitDisabled,
      isDirty: () => name.length > 0 || description.length > 0,
    }));

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
      // 2-col layout: form cards on the left, sticky Live Preview on
      // the right. The preview hugs the top of the viewport while the
      // operator scrolls through Source/Destination, Protocol/Ports
      // and Posture so the preview always reflects the field they
      // just touched. Below xl the preview falls under the form for
      // narrow screens.
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
        <div className="flex min-w-0 flex-col gap-4">
        {/* Card 1 — Identity. Mirrors handoff Identity: name + status
            tile side-by-side at the top, description spans the row
            below. Trades the modal's stacked-everything layout for
            the wider horizontal posture the page allows. */}
        <OzCard>
          <div className="grid grid-cols-1 gap-5 md:grid-cols-[minmax(0,1fr)_160px]">
            <div>
              <FieldLabel htmlFor="policy-name" required>
                Policy name
              </FieldLabel>
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

            <div>
              <FieldLabel>Status</FieldLabel>
              <EnableToggleButton
                value={enabled}
                onChange={setEnabled}
                disabled={fieldsDisabled}
              />
            </div>
          </div>

          <div className="mt-5">
            <FieldLabel htmlFor="policy-description" optional>
              Description
            </FieldLabel>
            <OzHelpText className="mb-2">
              Helps teammates understand intent.
            </OzHelpText>
            <OzTextarea
              id="policy-description"
              value={description}
              data-cy="policy-description"
              onChange={(e) => setDescription(e.target.value)}
              placeholder="e.g., Devs are allowed to access servers and servers are allowed to access Devs."
              rows={2}
              className="!min-h-[60px]"
              disabled={fieldsDisabled}
            />
          </div>
        </OzCard>

        {/* Card 2 — tabbed rules card. The legacy modal used to split
            "Policy" (Source/Destination + Protocol + Ports) and
            "Posture Checks" across two tabs; the page editor adopts
            the same shape inside a single OzCard so the two
            conceptual layers (network rules vs. peer prerequisites)
            stay distinct without inflating the card count. */}
        <OzCard flush>
          <OzTabs defaultValue="policy">
            <div className="px-[18px] pt-[14px]">
              <OzTabsList>
                <OzTabsTrigger value="policy">
                  <ArrowRightLeft size={14} />
                  Policy
                </OzTabsTrigger>
                <OzTabsTrigger value="ports">
                  <Shield size={14} />
                  Ports & Protocol
                </OzTabsTrigger>
                <OzTabsTrigger value="posture">
                  <ShieldCheck size={14} />
                  Posture Checks
                </OzTabsTrigger>
              </OzTabsList>
            </div>

            <OzTabsContent value="policy" className="px-[18px] pb-[18px] pt-3">
              <div className="grid grid-cols-1 items-start gap-6 md:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]">
                <div>
                  <FieldLabel hint="Peers in any of these groups can initiate the connection.">
                    Source · From
                  </FieldLabel>
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
                    side="top"
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
                <div className="md:hidden">
                  <PolicyDirection
                    value={direction}
                    onChange={setDirection}
                    disabled={destinationOnlyResources}
                    destinationResource={destinationResource}
                  />
                </div>

                <div>
                  <FieldLabel hint="Connections will be matched against these destination groups.">
                    Destination · To
                  </FieldLabel>
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
                    side="top"
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
                  heartbeats) will be dropped. Operationally-asymmetric
                  ICMP (destination-unreachable, fragmentation-needed) is
                  dropped too. Keep ALL bidirectional or split into
                  per-protocol rules if your apps depend on those.{" "}
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

            </OzTabsContent>

            <OzTabsContent value="ports" className="px-[18px] pb-[18px] pt-3">
              <div
                className="flex flex-wrap items-start justify-between gap-4"
                data-cy="protocol-wrapper"
              >
                <div className="min-w-0 flex-1">
                  <FieldLabel hint="Allow only specified network protocols. Select TCP or UDP to constrain ports.">
                    Protocol
                  </FieldLabel>
                </div>
                <div className="w-[140px] shrink-0">
                  <OzSelect
                    value={protocol}
                    onValueChange={(v) =>
                      handleProtocolChange(v as Protocol)
                    }
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
                <FieldLabel
                  hint={
                    portDisabled
                      ? "Switch the protocol on the Policy tab to TCP or UDP to constrain ports."
                      : "Empty = all ports for the chosen protocol."
                  }
                >
                  Ports
                </FieldLabel>
                <PortSelector
                  showAll
                  ports={ports}
                  onPortsChange={setPorts}
                  portRanges={portRanges}
                  onPortRangesChange={setPortRanges}
                  disabled={portDisabled}
                />
              </div>
            </OzTabsContent>

            <OzTabsContent
              value="posture"
              className="px-[18px] pb-[18px] pt-3"
            >
              <PostureCheckCardBody
                postureChecks={postureChecks}
                setPostureChecks={setPostureChecks}
                isLoading={isPostureChecksLoading}
              />
            </OzTabsContent>
          </OzTabs>
        </OzCard>
        </div>

        {/* Right rail — sticky Live Preview + Impact aligned with the
            top of the Identity card. Preview shows the policy shape;
            Impact tells the operator how many peers/services the
            policy actually touches today. */}
        <aside className="flex flex-col gap-4 xl:sticky xl:top-6 xl:self-start">
          <PolicyLivePreview
            name={name}
            enabled={enabled}
            direction={direction}
            sourceGroups={sourceGroups}
            destinationGroups={destinationGroups}
            destinationResource={destinationResource}
            protocol={protocol}
            ports={ports}
            portRanges={portRanges}
          />
          <PolicyImpact
            sourceGroups={sourceGroups}
            destinationGroups={destinationGroups}
            destinationResource={destinationResource}
          />
        </aside>
      </div>
    );
  },
);

export default PolicyEditorBody;

// PostureCheckCardBody — the inner content of the Posture Checks
// tab. Drops the standalone card wrapper (the parent OzTabsContent
// already supplies padding + surface) but keeps the helper-modal
// state and the minimal-table composition the legacy PostureCheckTab
// pioneered.
function PostureCheckCardBody({
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
    <>
      <p className="mb-4 max-w-2xl text-[12.5px] leading-[1.5] text-oz2-text-muted">
        Conditions a source peer must satisfy before this policy applies —
        OS, agent version, geofencing. Empty list means the policy applies
        unconditionally.
      </p>

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
    </>
  );
}

// FieldLabel — editor-only field caption. Mono caps + faint color +
// letter-spacing, matching the handoff FieldLabel helper. Hint text
// flows below in sans like the handoff version. Replaces the generic
// OzLabel in this surface so the editor reads with a more deliberate
// type hierarchy than the standard form labels elsewhere.
function FieldLabel({
  children,
  hint,
  htmlFor,
  required,
  optional,
}: {
  children: React.ReactNode;
  hint?: React.ReactNode;
  htmlFor?: string;
  required?: boolean;
  optional?: boolean;
}) {
  return (
    <div className="mb-2">
      <LabelPrimitive.Root
        htmlFor={htmlFor}
        className="inline-flex items-center gap-1.5 font-mono text-[11px] font-medium uppercase leading-none tracking-[0.1em] text-oz2-text-faint"
      >
        <span>{children}</span>
        {required && (
          <span aria-hidden className="text-oz2-err">
            *
          </span>
        )}
        {optional && (
          <span className="font-mono text-[10px] tracking-[0.06em] text-oz2-text-faint/70">
            optional
          </span>
        )}
      </LabelPrimitive.Root>
      {hint && (
        <div className="mt-1.5 font-sans text-[12px] leading-[1.45] text-oz2-text-muted normal-case tracking-normal">
          {hint}
        </div>
      )}
    </div>
  );
}

// EnableToggleButton — compact bordered tile matching the handoff
// Identity-card Status widget. Sits in a 160px column next to the
// policy name. The "switch" affordance inside is a styled span pair,
// not a real <button> — Radix Switch (used by OzSwitch elsewhere)
// renders as a button, and nesting button-in-button is invalid HTML
// and trips React's hydration check.
function EnableToggleButton({
  value,
  onChange,
  disabled,
}: {
  value: boolean;
  onChange: (v: boolean) => void;
  disabled: boolean;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={value}
      disabled={disabled}
      onClick={() => onChange(!value)}
      className={cn(
        "inline-flex h-[38px] w-full items-center gap-2.5 rounded-oz2-input border border-oz2-border bg-oz2-surface px-3 text-[13px] font-medium text-oz2-text transition-colors",
        "hover:border-oz2-border-strong hover:bg-oz2-hover",
        "disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-oz2-surface disabled:hover:border-oz2-border",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-oz2-acc/40",
      )}
    >
      <span
        aria-hidden
        className={cn(
          "inline-flex h-4 w-7 shrink-0 items-center rounded-full p-0.5 transition-colors",
          value ? "bg-oz2-acc" : "bg-oz2-border-strong",
        )}
      >
        <span
          className={cn(
            "block h-3 w-3 rounded-full bg-white shadow-[0_1px_2px_rgba(0,0,0,0.18)] transition-transform",
            value ? "translate-x-3" : "translate-x-0",
          )}
        />
      </span>
      {value ? "Enabled" : "Disabled"}
    </button>
  );
}
