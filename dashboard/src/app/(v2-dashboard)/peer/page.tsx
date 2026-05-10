"use client";

import FullTooltip from "@components/FullTooltip";
import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
  ModalTrigger,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import { PeerGroupSelector } from "@components/PeerGroupSelector";
import FullScreenLoading from "@components/ui/FullScreenLoading";
import LoginExpiredBadge from "@components/ui/LoginExpiredBadge";
import { PageNotFound } from "@components/ui/PageNotFound";
import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import TextWithTooltip from "@components/ui/TextWithTooltip";
import useRedirect from "@hooks/useRedirect";
import useFetchApi from "@utils/api";
import dayjs from "dayjs";
import { isEmpty, trim } from "lodash";
import {
  Barcode,
  Copy,
  Cpu,
  FlagIcon,
  Globe,
  History,
  Info,
  LockIcon,
  MapPin,
  MonitorSmartphoneIcon,
  NetworkIcon,
  PencilIcon,
  TerminalSquare,
  TimerResetIcon,
} from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { toASCII } from "punycode";
import React, { useMemo, useState } from "react";
import Skeleton from "react-loading-skeleton";
import { useSWRConfig } from "swr";
import RoundedFlag from "@/assets/countries/RoundedFlag";
import OpenzroIcon from "@/assets/icons/OpenzroIcon";
import PeerIcon from "@/assets/icons/PeerIcon";
import OzButton from "@/components/v2/OzButton";
import OzCard from "@/components/v2/OzCard";
import OzInput from "@/components/v2/OzInput";
import {
  OzTabs,
  OzTabsContent,
  OzTabsList,
  OzTabsTrigger,
} from "@/components/v2/OzTabs";
import { useCountries } from "@/contexts/CountryProvider";
import PeerProvider, { usePeer } from "@/contexts/PeerProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import RoutesProvider from "@/contexts/RoutesProvider";
import { useHasChanges } from "@/hooks/useHasChanges";
import type { Peer } from "@/interfaces/Peer";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import useGroupHelper from "@/modules/groups/useGroupHelper";
import { AccessiblePeersSection } from "@/modules/peer/AccessiblePeersSection";
import { PeerExpirationToggle } from "@/modules/peer/PeerExpirationToggle";
import { PeerNetworkRoutesSection } from "@/modules/peer/PeerNetworkRoutesSection";
import OzSettingsCard from "@/modules/settings/v2/OzSettingsCard";
import OzSettingsToggle from "@/modules/settings/v2/OzSettingsToggle";

// /peer — v2 chrome entry. Body keeps the legacy widgets unchanged
// (PeerExpirationToggle, FancyToggleSwitch, PeerInformationCard,
// EditNameModal, the Tabs nav for Network Routes / Accessible Peers).
// Only the wrapping page chrome flips: PageContainer + Breadcrumbs
// drop out (handled by V2DashboardLayout); legacy Buttons become
// OzButton. A deeper v2 paint of the per-peer widgets is tracked
// separately — too much surface to flip in one commit.

export default function PeerPage() {
  const queryParameter = useSearchParams();
  const { isRestricted } = usePermissions();
  const peerId = queryParameter.get("id");
  const {
    data: peer,
    isLoading,
    error,
  } = useFetchApi<Peer>("/peers/" + peerId, true);

  useRedirect("/peers", false, !peerId || isRestricted);

  const peerKey = useMemo(() => {
    const id = peer?.id ?? "";
    const ssh = peer?.ssh_enabled ? "1" : "0";
    const expiration = peer?.login_expiration_enabled ? "1" : "0";
    return `${id}-${ssh}-${expiration}`;
  }, [peer]);

  if (isRestricted) {
    return (
      <div className="space-y-6 p-8">
        <RestrictedAccess page={"Peer Information"} />
      </div>
    );
  }

  if (error)
    return (
      <PageNotFound
        title={error?.message}
        description={
          "The peer you are attempting to access cannot be found. It may have been deleted, or you may not have permission to view it. Please verify the URL or return to the dashboard."
        }
      />
    );

  return peer && !isLoading ? (
    <PeerProvider peer={peer} key={peerId}>
      <PeerOverview key={peerKey} />
    </PeerProvider>
  ) : (
    <FullScreenLoading />
  );
}

function PeerOverview() {
  const { peer } = usePeer();

  return (
    <RoutesProvider>
      <PeerGeneralInformation />
    </RoutesProvider>
  );
}

const PeerGeneralInformation = () => {
  const router = useRouter();
  const { mutate } = useSWRConfig();
  const { peer, user, peerGroups, openSSHDialog, update } = usePeer();
  const { permission } = usePermissions();
  const [ssh, setSsh] = useState(peer.ssh_enabled);
  const [name, setName] = useState(peer.name);
  const [showEditNameModal, setShowEditNameModal] = useState(false);
  const [loginExpiration, setLoginExpiration] = useState(
    peer.login_expiration_enabled,
  );
  const [inactivityExpiration, setInactivityExpiration] = useState(
    peer.inactivity_expiration_enabled,
  );
  const [selectedGroups, setSelectedGroups, { getAllGroupCalls }] =
    useGroupHelper({
      initial: peerGroups,
      peer,
    });

  const { hasChanges, updateRef: updateHasChangedRef } = useHasChanges([
    ssh,
    selectedGroups,
    loginExpiration,
    inactivityExpiration,
  ]);

  const updatePeer = async (newName?: string) => {
    let batchCall: Promise<any>[] = [];
    const groupCalls = getAllGroupCalls();

    if (permission.peers.update) {
      const updateRequest = update({
        name: newName ?? name,
        ssh,
        loginExpiration,
        inactivityExpiration,
      });
      batchCall = groupCalls ? [...groupCalls, updateRequest] : [updateRequest];
    } else {
      batchCall = [...groupCalls];
    }

    notify({
      title: name,
      description: "Peer was successfully saved",
      promise: Promise.all(batchCall).then(() => {
        mutate("/peers/" + peer.id);
        mutate("/groups");
        updateHasChangedRef([
          ssh,
          selectedGroups,
          loginExpiration,
          inactivityExpiration,
        ]);
      }),
      loadingMessage: "Saving the peer...",
    });
  };

  // Cancel / Save move into the v2 topbar slot so every page exposes
  // its primary action in the same place. Save's disabled state is
  // bound to hasChanges, so the slot re-registers on every render
  // that flips the dirty bit — useV2TopbarRight is reactive to its
  // node argument.
  useV2TopbarRight(
    <div className="flex items-center gap-2">
      <OzButton
        variant="default"
        type="button"
        onClick={() => router.push("/peers")}
      >
        Cancel
      </OzButton>
      <OzButton
        variant="primary"
        type="button"
        onClick={() => updatePeer()}
        disabled={
          !hasChanges || !permission.peers.read || !permission.groups.update
        }
      >
        Save Changes
      </OzButton>
    </div>,
  );

  // Default tab: Details. If the operator opens a peer page with no
  // permission to edit (groups + peers), every Details widget is
  // disabled anyway — they still see the same overview, so Details
  // stays the right default.
  const [tab, setTab] = useState("details");

  const lastSeenLabel = peer.connected
    ? "just now"
    : dayjs(peer.last_seen).fromNow();

  return (
    <>
      {/* Hero — handoff PeerDetailScreen shape. Status orb +
          identity block on the left; Save / Cancel CTAs on the right.
          A hairline border below separates the hero from the tabs
          band. */}
      <div className="px-8 pb-5 pt-8">
        <div className="flex max-w-6xl flex-wrap items-start justify-between gap-4">
          <div className="flex min-w-0 items-start gap-4">
            <div
              aria-hidden
              className="relative grid h-12 w-12 shrink-0 place-items-center rounded-[12px] border border-oz2-border-soft bg-oz2-bg-sunken text-oz2-text-2"
            >
              <PeerIcon size={20} />
              <span
                className={
                  "absolute -bottom-1 -right-1 h-3 w-3 rounded-full border-2 border-oz2-bg " +
                  (peer.connected ? "bg-oz2-ok" : "bg-oz2-text-faint")
                }
              />
            </div>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h1 className="text-[24px] font-semibold tracking-tight text-oz2-text">
                  <TextWithTooltip text={name} maxChars={30} />
                </h1>
                <LoginExpiredBadge loginExpired={peer.login_expired} />
                {permission.peers.update && (
                  <Modal
                    open={showEditNameModal}
                    onOpenChange={setShowEditNameModal}
                  >
                    <ModalTrigger>
                      <span
                        aria-label="Edit peer name"
                        className="inline-grid h-7 w-7 cursor-pointer place-items-center rounded-[8px] border border-oz2-border bg-oz2-surface text-oz2-text-2 transition-colors hover:border-oz2-border-strong hover:bg-oz2-hover hover:text-oz2-text"
                      >
                        <PencilIcon size={13} />
                      </span>
                    </ModalTrigger>
                    <EditNameModal
                      onSuccess={(newName) => {
                      updatePeer(newName).then(() => {
                        setName(newName);
                        setShowEditNameModal(false);
                      });
                      }}
                      peer={peer}
                      initialName={name}
                      key={showEditNameModal ? 1 : 0}
                    />
                  </Modal>
                )}
              </div>
              <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12.5px] text-oz2-text-muted">
                <span className="font-mono text-oz2-text-2">{peer.ip}</span>
                <span className="text-oz2-text-faint">·</span>
                <span>{peer.os}</span>
                {user?.email && (
                  <>
                    <span className="text-oz2-text-faint">·</span>
                    <span>
                      Owned by{" "}
                      <span className="text-oz2-text-2">{user.email}</span>
                    </span>
                  </>
                )}
                <span className="text-oz2-text-faint">·</span>
                <span>Last seen {lastSeenLabel}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Tabs — Details holds the edit form; the other two tabs host
          their respective list sections. */}
      <OzTabs value={tab} onValueChange={setTab}>
        <div className="px-8">
          <OzTabsList>
            <OzTabsTrigger value="details">
              <Info size={13} />
              Details
            </OzTabsTrigger>
            {permission.routes.read && (
              <OzTabsTrigger value="network-routes">
                <NetworkIcon size={13} />
                Network Routes
              </OzTabsTrigger>
            )}
            {peer?.id && permission.peers.read && (
              <OzTabsTrigger value="accessible-peers">
                <MonitorSmartphoneIcon size={13} />
                Accessible Peers
              </OzTabsTrigger>
            )}
          </OzTabsList>
        </div>

        <OzTabsContent value="details">
          <div className="px-8 py-6">
            <div className="flex w-full max-w-6xl flex-wrap items-start gap-10 xl:flex-nowrap">
              <PeerInformationCard peer={peer} />

              <div className="flex flex-col gap-5 transition-all lg:w-1/2">
                <OzSettingsCard
                  title={
                    <span className="inline-flex items-center gap-2">
                      <TimerResetIcon size={14} />
                      Session Expiration
                    </span>
                  }
                  sub="Force this peer to re-authenticate through SSO when its session expires. Setup-key peers can't be expired (no user to sign in)."
                >
                  <PeerExpirationToggle
                    peer={peer}
                    value={loginExpiration}
                    onChange={(state) => {
                      setLoginExpiration(state);
                      if (!state) setInactivityExpiration(false);
                    }}
                  />

                  {permission.peers.update &&
                    !!peer?.user_id &&
                    loginExpiration && (
                      <div className="flex flex-col gap-4 rounded-oz2-card border border-oz2-border-soft bg-oz2-bg-sunken p-4">
                        <PeerExpirationToggle
                          peer={peer}
                          value={inactivityExpiration}
                          onChange={setInactivityExpiration}
                          title="Require login after disconnect"
                          description="Enable to require authentication after users disconnect from management for 10 minutes."
                          nested
                        />
                      </div>
                    )}
                </OzSettingsCard>

                <OzSettingsCard
                  title={
                    <span className="inline-flex items-center gap-2">
                      <TerminalSquare size={14} />
                      SSH Access
                    </span>
                  }
                  sub="Run an SSH server on this peer so operators can reach the machine through the mesh with a standard shell."
                >
                  <FullTooltip
                    content={
                      <div className="flex items-center gap-2 !text-nb-gray-300 text-xs">
                        <LockIcon size={14} />
                        <span>
                          {`You don't have the required permissions to update this setting.`}
                        </span>
                      </div>
                    }
                    interactive={false}
                    className="w-full block"
                    disabled={!permission.peers.update}
                  >
                    <OzSettingsToggle
                      value={ssh}
                      disabled={!permission.peers.update}
                      onChange={(set) =>
                        !set
                          ? setSsh(false)
                          : openSSHDialog().then((confirm) =>
                              setSsh(confirm),
                            )
                      }
                      label="Enable SSH server"
                      desc="The openZro client opens an SSH listener bound to its mesh IP."
                    />
                  </FullTooltip>
                </OzSettingsCard>

                {permission.groups.read && (
                  <OzSettingsCard
                    title="Assigned Groups"
                    sub="Groups control what this peer can reach across the mesh. A peer inherits every policy that targets one of its groups."
                  >
                    <PeerGroupSelector
                      disabled={!permission.groups.update}
                      onChange={setSelectedGroups}
                      values={selectedGroups}
                      hideAllGroup={true}
                      peer={peer}
                    />
                  </OzSettingsCard>
                )}
              </div>
            </div>
          </div>
        </OzTabsContent>

        {permission.routes.read && (
          <OzTabsContent value="network-routes">
            <PeerNetworkRoutesSection peer={peer} />
          </OzTabsContent>
        )}

        {peer?.id && permission.peers.read && (
          <OzTabsContent value="accessible-peers">
            <AccessiblePeersSection peerID={peer.id} />
          </OzTabsContent>
        )}
      </OzTabs>
    </>
  );
};

function PeerInformationCard({ peer }: Readonly<{ peer: Peer }>) {
  const { isLoading, getRegionByPeer } = useCountries();

  const countryText = useMemo(() => {
    return getRegionByPeer(peer);
  }, [getRegionByPeer, peer]);

  const lastSeenText = peer.connected
    ? "just now"
    : dayjs(peer.last_seen).format("D MMMM, YYYY [at] h:mm A") +
      " (" +
      dayjs().to(peer.last_seen) +
      ")";

  return (
    <OzCard flush className="w-full xl:w-1/2">
      <ul className="divide-y divide-oz2-border-soft">
        <PeerInfoRow
          icon={<MapPin size={14} />}
          label="openZro IP-Address"
          value={peer.ip}
          copy
          copyToast="openZro IP-Address"
          mono
        />
        <PeerInfoRow
          icon={<NetworkIcon size={14} />}
          label="Public IP-Address"
          value={peer.connection_ip}
          copy
          copyToast="Public IP-Address"
          mono
        />
        <PeerInfoRow
          icon={<Globe size={14} />}
          label="Domain Name"
          value={peer.dns_label}
          extraValues={peer.extra_dns_labels}
          copy
          copyToast="DNS label"
          mono
        />
        <PeerInfoRow
          icon={<MonitorSmartphoneIcon size={14} />}
          label="Hostname"
          value={peer.hostname}
          copy
          copyToast="Hostname"
          mono
        />
        <PeerInfoRow
          icon={<FlagIcon size={14} />}
          label="Region"
          value={
            isEmpty(peer.country_code) ? (
              "Unknown"
            ) : isLoading ? (
              <Skeleton width={140} />
            ) : (
              <span className="inline-flex items-center gap-2">
                <RoundedFlag country={peer.country_code} size={12} />
                {countryText}
              </span>
            )
          }
        />
        <PeerInfoRow
          icon={<Cpu size={14} />}
          label="Operating System"
          value={peer.os}
        />
        {peer.serial_number && peer.serial_number !== "" && (
          <PeerInfoRow
            icon={<Barcode size={14} />}
            label="Serial Number"
            value={peer.serial_number}
          />
        )}
        <PeerInfoRow
          icon={<History size={14} />}
          label="Last seen"
          value={lastSeenText}
        />
        <PeerInfoRow
          icon={<OpenzroIcon size={14} />}
          label="Agent Version"
          value={peer.version}
        />
        {peer.ui_version && (
          <PeerInfoRow
            icon={<OpenzroIcon size={14} />}
            label="UI Version"
            value={peer.ui_version?.replace("openzro-desktop-ui/", "")}
          />
        )}
      </ul>
    </OzCard>
  );
}

// PeerInfoRow — single key/value row inside PeerInformationCard's
// divided list. `copy` turns the row into a click-to-copy target with
// a notify toast; `mono` flips the value to the monospace font for
// IDs/addresses. `extraValues` stacks below the primary value (used
// for the additional DNS labels of multi-domain peers).
function PeerInfoRow({
  icon,
  label,
  value,
  extraValues,
  copy,
  copyToast,
  mono,
}: {
  icon: React.ReactNode;
  label: React.ReactNode;
  value: React.ReactNode;
  extraValues?: string[];
  copy?: boolean;
  copyToast?: string;
  mono?: boolean;
}) {
  const stringValue = typeof value === "string" ? value : null;
  const canCopy = !!(copy && stringValue);

  const onCopy = () => {
    if (!canCopy || !stringValue) return;
    navigator.clipboard?.writeText(stringValue).then(() => {
      notify({
        title: copyToast ?? "Copied",
        description: `${copyToast ?? "Value"} has been copied to clipboard.`,
        promise: Promise.resolve(),
        loadingMessage: "",
      });
    });
  };

  const hasExtras = !!(extraValues && extraValues.length > 0);

  return (
    <li
      className={
        "flex flex-wrap gap-3 px-[18px] py-3 text-[13.5px] " +
        (hasExtras ? "items-start" : "items-center") +
        " " +
        (canCopy
          ? "cursor-pointer transition-colors hover:bg-oz2-hover/40"
          : "")
      }
      onClick={canCopy ? onCopy : undefined}
      role={canCopy ? "button" : undefined}
      tabIndex={canCopy ? 0 : undefined}
      onKeyDown={
        canCopy
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onCopy();
              }
            }
          : undefined
      }
    >
      <span className="inline-flex min-w-[180px] items-center gap-2 text-oz2-text-muted">
        {icon}
        {label}
      </span>
      <span className="flex min-w-0 flex-1 items-center justify-end gap-2 text-right">
        <span className="flex min-w-0 flex-col items-end gap-0.5">
          <span
            className={
              "truncate text-oz2-text " + (mono ? "font-mono text-[12.5px]" : "")
            }
          >
            {value}
          </span>
          {extraValues?.map((extra) => (
            <span
              key={extra}
              className={
                "truncate text-oz2-text-2 " +
                (mono ? "font-mono text-[12px]" : "text-[12.5px]")
              }
            >
              {extra}
            </span>
          ))}
        </span>
        {canCopy && (
          <Copy size={13} className="shrink-0 text-oz2-text-faint" />
        )}
      </span>
    </li>
  );
}

interface ModalProps {
  onSuccess: (name: string) => void;
  peer: Peer;
  initialName: string;
}

function EditNameModal({ onSuccess, peer, initialName }: Readonly<ModalProps>) {
  const [name, setName] = useState(initialName);

  const isDisabled = useMemo(() => {
    if (name === peer.name) return true;
    const trimmedName = trim(name);
    return trimmedName.length === 0;
  }, [name, peer]);

  const domainNamePreview = useMemo(() => {
    let punyName = toASCII(name.toLowerCase());
    punyName = punyName.replace(/[^a-z0-9]/g, "-");
    let domain = "";
    if (peer.dns_label) {
      const labelList = peer.dns_label.split(".");
      if (labelList.length > 1) {
        labelList.splice(0, 1);
        domain = "." + labelList.join(".");
      }
    }
    return punyName + domain;
  }, [name, peer]);

  return (
    <ModalContent maxWidthClass={"max-w-md"}>
      <form>
        <ModalHeader
          title={"Edit Peer Name"}
          description={"Set an easily identifiable name for your peer."}
          color={"blue"}
        />

        <div className="flex flex-col gap-4 p-6">
          <OzInput
            placeholder="e.g., AWS Servers"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
          />
          <div className="rounded-oz2-card border border-oz2-border-soft bg-oz2-bg-sunken px-4 py-3">
            <div className="inline-flex items-center gap-2 text-[12.5px] font-medium text-oz2-text-2">
              <Globe size={13} />
              Domain Name Preview
            </div>
            <p className="mt-1 text-[11.5px] leading-[1.45] text-oz2-text-faint">
              If the domain name already exists, an increment-number suffix is
              appended.
            </p>
            <p className="mt-2 break-all font-mono text-[13px] text-oz2-acc-text">
              {domainNamePreview}
            </p>
          </div>
        </div>

        <ModalFooter className="items-center" separator={false}>
          <div className="flex w-full justify-end gap-3">
            <ModalClose asChild>
              <OzButton variant="default" type="button">
                Cancel
              </OzButton>
            </ModalClose>

            <OzButton
              variant="primary"
              type="submit"
              onClick={() => onSuccess(name)}
              disabled={isDisabled}
            >
              Save
            </OzButton>
          </div>
        </ModalFooter>
      </form>
    </ModalContent>
  );
}
