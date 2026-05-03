"use client";

import Breadcrumbs from "@components/Breadcrumbs";
import Button from "@components/Button";
import InlineLink from "@components/InlineLink";
import Paragraph from "@components/Paragraph";
import SkeletonTable from "@components/skeletons/SkeletonTable";
import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import { usePortalElement } from "@hooks/usePortalElement";
import { ExternalLinkIcon, FolderGit2Icon, PlusCircle } from "lucide-react";
import React, { lazy, Suspense, useState } from "react";
import TeamIcon from "@/assets/icons/TeamIcon";
import { usePermissions } from "@/contexts/PermissionsProvider";
import PageContainer from "@/layouts/PageContainer";
import CreateGroupModal from "@/modules/groups/CreateGroupModal";

const GroupsTable = lazy(() => import("@/modules/groups/GroupsTable"));

export default function TeamGroupsPage() {
  const { permission } = usePermissions();
  const { ref: headingRef, portalTarget } =
    usePortalElement<HTMLHeadingElement>();

  const [createOpen, setCreateOpen] = useState(false);

  return (
    <PageContainer>
      <CreateGroupModal open={createOpen} onOpenChange={setCreateOpen} />

      <div className={"p-default py-6"}>
        <Breadcrumbs>
          <Breadcrumbs.Item
            href={"/team"}
            label={"Team"}
            icon={<TeamIcon size={13} />}
          />
          <Breadcrumbs.Item
            href={"/team/groups"}
            label={"Groups"}
            active
            icon={<FolderGit2Icon size={14} />}
          />
        </Breadcrumbs>
        <div className={"flex items-start justify-between max-w-6xl"}>
          <div>
            <h1 ref={headingRef}>Groups</h1>
            <Paragraph>
              Groups bundle peers, resources and users so policies, routes and
              setup keys can target them by name. Groups synced from your IdP
              (SCIM/JWT) are read-only.
            </Paragraph>
            <Paragraph>
              Learn more about
              <InlineLink
                href={"https://docs.openzro.io/how-to/manage-network-access"}
                target={"_blank"}
              >
                Groups
                <ExternalLinkIcon size={12} />
              </InlineLink>
              in our documentation.
            </Paragraph>
          </div>
          <Button
            variant={"primary"}
            onClick={() => setCreateOpen(true)}
            disabled={!permission.groups.create}
            data-cy={"create-group"}
          >
            <PlusCircle size={16} />
            New Group
          </Button>
        </div>
      </div>
      <RestrictedAccess page={"Groups"} hasAccess={permission.groups.read}>
        <Suspense fallback={<SkeletonTable />}>
          <GroupsTable headingTarget={portalTarget} />
        </Suspense>
      </RestrictedAccess>
    </PageContainer>
  );
}
