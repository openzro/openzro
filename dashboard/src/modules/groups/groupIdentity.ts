import { Group, GroupIssued } from "@/interfaces/Group";

type GroupIdentity = Pick<Group, "id" | "issued" | "name">;

export function groupIssued(issued?: GroupIssued): GroupIssued {
  return issued ?? GroupIssued.API;
}

export function groupIdentityKey(group?: GroupIdentity): string {
  if (!group) return "group:none";
  if (group.id) return `group:id:${group.id}`;
  return `group:new:${groupIssued(group.issued)}:${group.name}`;
}

export function groupIssuerNameKey(group?: GroupIdentity): string {
  if (!group) return "group:none";
  return `group:issuer-name:${groupIssued(group.issued)}:${group.name}`;
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

export function groupIssuedLabel(issued?: GroupIssued): string {
  switch (groupIssued(issued)) {
    case GroupIssued.JWT:
      return "JWT";
    case GroupIssued.INTEGRATION:
      return "SCIM";
    case GroupIssued.API:
    default:
      return "API";
  }
}

export function groupIssuedDescription(issued?: GroupIssued): string {
  switch (groupIssued(issued)) {
    case GroupIssued.JWT:
      return "Created from JWT group claims. Membership is driven by the IdP.";
    case GroupIssued.INTEGRATION:
      return "Provisioned by SCIM. Membership changes may be overwritten by the IdP.";
    case GroupIssued.API:
    default:
      return "Created manually in the dashboard or through the API.";
  }
}
