import { Permissions } from "@/interfaces/Permission";

export interface User {
  id: string;
  email?: string;
  name: string;
  role: Role;
  status: string;
  auto_groups: string[];
  is_current?: boolean;
  is_service_user?: boolean;
  is_blocked?: boolean;
  last_login?: Date;
  permissions: Permissions;
  // issued tells the dashboard how this user was created. "integration"
  // means an external IdP pushed the user via SCIM — manual edits will
  // be overwritten on the next sync, so the UI shows a SCIM badge.
  issued?: UserIssued;
  // local_credential_state is set only for local users, whose email and
  // password live in openZro's embedded Dex (ADR-0024, ADR-0026).
  local_credential_state?: LocalCredentialState;
  // mfa_enrolled and mfa_locked_until are returned to admins only. A
  // lockout follows repeated wrong 2FA codes and ends on its own.
  mfa_enrolled?: boolean;
  mfa_locked_until?: string;
}

export type LocalCredentialState = "active" | "deactivated";

export enum UserIssued {
  API = "api",
  INTEGRATION = "integration",
}

export enum Role {
  User = "user",
  Admin = "admin",
  Owner = "owner",
  BillingAdmin = "billing_admin",
  Auditor = "auditor",
  NetworkAdmin = "network_admin",
}
