import { Group, GroupIssued } from "@/interfaces/Group";

export const unknownGroupIssued = "unknown";

export type GroupOrigin = GroupIssued | typeof unknownGroupIssued;

type GroupIdentity = Pick<Group, "id" | "name"> & {
  issued?: GroupIssued | string;
};

const knownGroupIssued = new Set<string>(Object.values(GroupIssued));

export function groupIssued(issued?: GroupIssued | string): GroupOrigin {
  if (!issued) return GroupIssued.API;
  if (knownGroupIssued.has(issued)) return issued as GroupIssued;
  return unknownGroupIssued;
}

function groupIssuerKey(issued?: GroupIssued | string): string {
  const origin = groupIssued(issued);
  if (origin === unknownGroupIssued && issued) return `${origin}:${issued}`;
  return origin;
}

export function groupIdentityKey(group?: GroupIdentity): string {
  if (!group) return "group:none";
  if (group.id) return `group:id:${group.id}`;
  return `group:new:${groupIssuerKey(group.issued)}:${group.name}`;
}

export function groupIssuerNameKey(group?: GroupIdentity): string {
  if (!group) return "group:none";
  return `group:issuer-name:${groupIssuerKey(group.issued)}:${group.name}`;
}

export function sameGroupIdentity(
  left?: GroupIdentity,
  right?: GroupIdentity,
): boolean {
  if (!left || !right) return false;
  if (left.id && right.id) return left.id === right.id;
  return groupIssuerNameKey(left) === groupIssuerNameKey(right);
}

export function resolveGroupID(
  selectedGroup: GroupIdentity,
  resolvedGroups: Group[],
): string | undefined {
  if (selectedGroup.id) return selectedGroup.id;
  return resolvedGroups.find((group) => sameGroupIdentity(group, selectedGroup))
    ?.id;
}

export function groupSearchText(group: GroupIdentity): string {
  return `${group.name} ${groupIssuedLabel(group.issued)}`;
}

export function groupIssuedLabel(issued?: GroupIssued | string): string {
  switch (groupIssued(issued)) {
    case GroupIssued.JWT:
      return "JWT";
    case GroupIssued.INTEGRATION:
      return "SCIM";
    case GroupIssued.API:
      return "API";
    case unknownGroupIssued:
      return "Unknown";
  }
}

export function groupIssuedDescription(issued?: GroupIssued | string): string {
  switch (groupIssued(issued)) {
    case GroupIssued.JWT:
      return "Created from JWT group claims. Membership is driven by the IdP.";
    case GroupIssued.INTEGRATION:
      return "Provisioned by SCIM. Membership changes may be overwritten by the IdP.";
    case GroupIssued.API:
      return "Created manually in the dashboard or through the API.";
    case unknownGroupIssued:
      return "Origin is not recognized by this dashboard version. Treat it as externally managed until the dashboard is upgraded.";
  }
}
