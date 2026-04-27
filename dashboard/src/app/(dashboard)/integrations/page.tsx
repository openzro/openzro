"use client";

import { RestrictedAccess } from "@components/ui/RestrictedAccess";
import React from "react";
import { usePermissions } from "@/contexts/PermissionsProvider";
import PageContainer from "@/layouts/PageContainer";
import IntegrationsPage from "@/modules/integrations/IntegrationsPage";

export default function Integrations() {
  const { permission } = usePermissions();

  return (
    <PageContainer>
      <RestrictedAccess
        page={"Integrations"}
        hasAccess={permission.settings.read}
      >
        <IntegrationsPage />
      </RestrictedAccess>
    </PageContainer>
  );
}
