"use client";

import InlineLink from "@components/InlineLink";
import { ChevronLeftIcon, ExternalLinkIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import * as React from "react";
import { useRef, useState } from "react";
import OzButton from "@/components/v2/OzButton";
import { usePermissions } from "@/contexts/PermissionsProvider";
import { Group } from "@/interfaces/Group";
import { Policy } from "@/interfaces/Policy";
import { PostureCheck } from "@/interfaces/PostureCheck";
import { useV2TopbarRight } from "@/layouts/V2DashboardLayout";
import PolicyEditorBody, {
  type PolicyEditorHandle,
} from "@/modules/access-control/v2/PolicyEditorBody";

// PolicyEditorShell — page chrome for /access-control/new and
// /access-control/[id]. Provides the header (back link + H1 + sub),
// hosts the PolicyEditorBody (3 stacked cards), and parks the
// Cancel / Save buttons in the v2 topbar slot via useV2TopbarRight.
//
// The shell stays thin on purpose: validation + submit live inside
// PolicyEditorBody (driven by the useAccessControl hook). The parent
// only needs to forward the existing policy and a back-route, and the
// shell handles success navigation.

type Props = {
  policy?: Policy;
  // Optional seed data used by the "Create policy for this route"
  // flow when it eventually moves off the legacy modal — undefined for
  // a normal blank create.
  initialDestinationGroups?: Group[] | string[];
  initialName?: string;
  initialDescription?: string;
  postureCheckTemplates?: PostureCheck[];
};

export default function PolicyEditorShell({
  policy,
  initialDestinationGroups,
  initialName,
  initialDescription,
  postureCheckTemplates,
}: Readonly<Props>) {
  const router = useRouter();
  const { permission } = usePermissions();
  const editorRef = useRef<PolicyEditorHandle>(null);
  const [saving, setSaving] = useState(false);

  const goBack = () => router.push("/access-control");

  const handleSuccess = () => {
    setSaving(false);
    goBack();
  };

  // useAccessControl.submit() does not surface errors to its caller —
  // the failing API path shows its own toast and onSuccess never
  // fires. To avoid the Save button getting stuck on "Saving…" when
  // the server rejects the request, a 6s safety net resets the state.
  // The happy path navigates away in well under a second, so this
  // timer is invisible whenever the save succeeds.
  const handleSave = () => {
    if (!editorRef.current) return;
    if (editorRef.current.isSubmitDisabled()) return;
    setSaving(true);
    editorRef.current.submit();
    window.setTimeout(() => setSaving(false), 6000);
  };

  // `policy` may carry a seed (rules pre-populated by an upstream
  // flow, e.g. RouteModal's "Create policy for this route" prompt)
  // without an id. Distinguish seed-create from real edit by id so
  // header + save button + permission gate read correctly.
  const isEdit = !!policy?.id;
  const saveDisabled =
    saving || (isEdit ? !permission.policies.update : !permission.policies.create);

  // Topbar slot: Cancel + Save. Mirrors the SetupKey / Network page
  // pattern. The slot is reactive so the disabled state updates as the
  // operator fills the form — useV2TopbarRight re-injects when the
  // ReactNode identity changes, which is fine here because each render
  // produces a new node.
  useV2TopbarRight(
    <>
      <OzButton variant="default" onClick={goBack} disabled={saving}>
        Cancel
      </OzButton>
      <OzButton
        variant="primary"
        onClick={handleSave}
        disabled={saveDisabled}
        data-cy="submit-policy"
      >
        {saving ? "Saving…" : isEdit ? "Save changes" : "Add policy"}
      </OzButton>
    </>,
  );

  return (
    <div className="space-y-6 p-8">
      <header>
        <button
          type="button"
          onClick={goBack}
          className="inline-flex items-center gap-1 text-[12.5px] text-oz2-text-muted transition-colors hover:text-oz2-text"
        >
          <ChevronLeftIcon size={14} />
          Back to Access Control
        </button>
        <h1 className="mt-2 text-[24px] font-semibold tracking-tight">
          {isEdit ? "Edit policy" : "New access policy"}
        </h1>
        <p className="mt-1 max-w-2xl text-[14px] leading-[1.55] text-oz2-text-muted">
          Define how peers reach each other across the mesh. Source groups
          initiate connections to destination groups subject to the protocol,
          ports and posture conditions below. Changes apply within ~10 seconds
          of saving.{" "}
          <InlineLink
            href="https://docs.openzro.io/how-to/manage-network-access"
            target="_blank"
          >
            Learn more
            <ExternalLinkIcon size={12} />
          </InlineLink>
        </p>
      </header>

      {/* max-w-7xl fits the form col (Posture's minimal table needs
          real width) plus the 340px Live Preview rail at xl. Below xl
          the rail falls below the form so cards still get full width
          on narrower screens. */}
      <div className="max-w-7xl">
        <PolicyEditorBody
          ref={editorRef}
          policy={policy}
          initialDestinationGroups={initialDestinationGroups}
          initialName={initialName}
          initialDescription={initialDescription}
          postureCheckTemplates={postureCheckTemplates}
          onSuccess={handleSuccess}
        />
      </div>
    </div>
  );
}
