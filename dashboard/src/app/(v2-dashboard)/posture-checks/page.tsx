"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import useFetchApi from "@utils/api";
import React, { lazy, Suspense } from "react";
import GroupsProvider from "@/contexts/GroupsProvider";
import { usePermissions } from "@/contexts/PermissionsProvider";
import PoliciesProvider from "@/contexts/PoliciesProvider";
import { PostureCheck } from "@/interfaces/PostureCheck";

const PostureCheckTable = lazy(
  () => import("@/modules/posture-checks/table/PostureCheckTable"),
);

export default function PostureChecksPage() {
  const { permission } = usePermissions();
  const { data: postureChecks, isLoading } =
    useFetchApi<PostureCheck[]>("/posture-checks");

  return (
    <GroupsProvider>
      <div className="space-y-6 p-8">
        <header>
          <h1 className="text-[24px] font-semibold tracking-tight">
            Posture Checks
          </h1>
          <p className="mt-1 max-w-2xl text-[14px] text-oz2-text-muted">
            Layer device-state requirements on top of access policies — block
            connections from non-compliant peers (OS version, geo, MDM/EDR
            posture, etc.) before they reach the data plane.{" "}
            <a
              href="https://docs.openzro.io/how-to/manage-posture-checks"
              target="_blank"
              rel="noopener noreferrer"
              className="text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Learn more
            </a>
            .
          </p>
        </header>

        <RestrictedAccess
          page={"Posture Checks"}
          hasAccess={permission.policies.read}
        >
          <PoliciesProvider>
            <Suspense fallback={null}>
              <PostureCheckTable
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
