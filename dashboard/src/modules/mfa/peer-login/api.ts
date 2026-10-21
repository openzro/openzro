// Client for the device-login approval endpoints (ADR-0028 D4). They are
// public: the approval link's token, sent as a bearer, is the only
// credential, so these calls use a bare fetch rather than the dashboard's
// session-bound API helpers. Nothing is cached and no referrer is sent.

export type PeerLoginState =
  | "pending"
  | "approved"
  | "denied"
  | "consumed"
  | "expired";

export interface PeerLoginApproval {
  state: PeerLoginState;
  hostname: string;
  os: string;
  ip: string;
  geo_country?: string;
  geo_city?: string;
  new_peer: boolean;
  key_fingerprint: string;
  masked_email: string;
  account_domain: string;
  created_at: string;
  expires_at: string;
}

// What the page does with a load or a deny.
export type LoadResult =
  | { kind: "ok"; approval: PeerLoginApproval }
  | { kind: "invalid" } // 401: bad link or past its deadline
  | { kind: "gone" } // 412: the attempt no longer exists or changed state
  | { kind: "error" };

// What the page does with a code.
export type ApproveResult =
  | { kind: "approved" }
  | { kind: "wrong_code" }
  | { kind: "code_reused" }
  | { kind: "locked"; lockedUntil?: string }
  | { kind: "invalid" }
  | { kind: "gone" }
  | { kind: "error" };

function request(
  apiOrigin: string,
  path: string,
  token: string,
  init: RequestInit,
) {
  return fetch(`${apiOrigin}/api/mfa/peer-login${path}`, {
    ...init,
    cache: "no-store",
    referrerPolicy: "no-referrer",
    headers: {
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      Authorization: `Bearer ${token}`,
    },
  });
}

async function toLoadResult(res: Response): Promise<LoadResult> {
  if (res.ok)
    return { kind: "ok", approval: (await res.json()) as PeerLoginApproval };
  if (res.status === 401) return { kind: "invalid" };
  if (res.status === 412) return { kind: "gone" };
  return { kind: "error" };
}

export async function loadApproval(
  apiOrigin: string,
  token: string,
): Promise<LoadResult> {
  try {
    return await toLoadResult(
      await request(apiOrigin, "", token, { method: "GET" }),
    );
  } catch {
    return { kind: "error" };
  }
}

export async function denyApproval(
  apiOrigin: string,
  token: string,
): Promise<LoadResult> {
  try {
    return await toLoadResult(
      await request(apiOrigin, "/deny", token, { method: "POST" }),
    );
  } catch {
    return { kind: "error" };
  }
}

// approveWithCode sends the code; an empty code repeats an approval that
// already happened.
export async function approveWithCode(
  apiOrigin: string,
  token: string,
  code: string,
): Promise<ApproveResult> {
  try {
    const res = await request(apiOrigin, "/approve", token, {
      method: "POST",
      body: JSON.stringify({ code }),
    });
    if (res.status === 401) return { kind: "invalid" };
    if (res.status === 412) return { kind: "gone" };
    if (![200, 422, 423].includes(res.status)) return { kind: "error" };
    const body = (await res.json()) as {
      approved: boolean;
      reason?: "wrong_code" | "code_reused" | "locked";
      locked_until?: string;
    };
    if (body.approved) return { kind: "approved" };
    if (body.reason === "locked")
      return { kind: "locked", lockedUntil: body.locked_until };
    if (body.reason === "code_reused") return { kind: "code_reused" };
    return { kind: "wrong_code" };
  } catch {
    return { kind: "error" };
  }
}
