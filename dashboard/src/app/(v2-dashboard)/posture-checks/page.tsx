"use client";

import InlineLink from "@components/InlineLink";
import SkeletonTable from "@components/skeletons/SkeletonTable";
import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import { usePortalElement } from "@hooks/usePortalElement";
import useFetchApi from "@utils/api";
import { ExternalLinkIcon } from "lucide-react";
import React, { lazy, Suspense } from "react";
import GroupsProvider from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import PoliciesProvider from "@/contexts/PoliciesProvider";
import { PostureCheck } from "@/interfaces/PostureCheck";

// /posture-checks — v2 chrome entry. Body still renders the legacy
// PostureCheckTable; a deeper v2 paint of the table itself is
// deferred. Wrapper strips PageContainer + Breadcrumbs (V2 chrome
// handles both) and renders the page header in v2 paint.

const PostureCheckTable = lazy(
  () => import("@/modules/posture-checks/table/PostureCheckTable"),
);

export default function PostureChecksPage() {
  const { permission } = usePermissions();
  const { data: postureChecks, isLoading } =
    useFetchApi<PostureCheck[]>("/posture-checks");

  const { ref: headingRef, portalTarget } =
    usePortalElement<HTMLHeadingElement>();

  return (
    <GroupsProvider>
      <div className="space-y-6 p-8">
        <header>
          <h1
            ref={headingRef}
            className="text-[24px] font-semibold tracking-tight"
          >
            Posture Checks
          </h1>
          <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
            Layer device-state requirements on top of access policies — block
            connections from non-compliant peers (OS version, geo, MDM/EDR
            posture, etc.) before they reach the data plane.{" "}
            <InlineLink
              href="https://docs.openzro.io/how-to/manage-posture-checks"
              target="_blank"
            >
              Learn more
              <ExternalLinkIcon size={11} />
            </InlineLink>
          </p>
        </header>

        <RestrictedAccess
          page={"Posture Checks"}
          hasAccess={permission.policies.read}
        >
          <PoliciesProvider>
            <Suspense fallback={<SkeletonTable />}>
              <PostureCheckTable
                headingTarget={portalTarget}
                isLoading={isLoading}
                postureChecks={postureChecks}
              />
            </Suspense>
          </PoliciesProvider>
        </RestrictedAccess>
      </div>
    </GroupsProvider>
  );
}
