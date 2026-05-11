"use client";

import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import { CableIcon } from "lucide-react";
import React, { useEffect, useState } from "react";
import { useSWRConfig } from "swr";
import OzButton from "@/components/v2/OzButton";
import OzInput from "@/components/v2/OzInput";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import {
  OzSelect,
  OzSelectContent,
  OzSelectItem,
  OzSelectTrigger,
  OzSelectValue,
} from "@/components/v2/OzSelect";
import OzTextarea from "@/components/v2/OzTextarea";
import {
  ActivityExporter,
  ActivityExporterInput,
  ActivityExporterType,
} from "@/interfaces/ActivityExporter";

type Props = {
  open: boolean;
  setOpen: (open: boolean) => void;
  existing?: ActivityExporter | null;
};

const PLACEHOLDER_KEEP = "(unchanged — leave empty to keep)";

export default function ActivityExporterModal({
  open,
  setOpen,
  existing,
}: Readonly<Props>) {
  const isEdit = !!existing;
  const [name, setName] = useState("");
  const [type, setType] = useState<ActivityExporterType>("http");
  const [enabled, setEnabled] = useState(true);
  const [template, setTemplate] = useState("");
  const [saving, setSaving] = useState(false);
  const [validating, setValidating] = useState(false);

  // HTTP fields
  const [httpURL, setHttpURL] = useState("");
  const [httpHeadersJSON, setHttpHeadersJSON] = useState("");

  // Datadog fields
  const [ddSite, setDdSite] = useState("us1");
  const [ddURL, setDdURL] = useState("");
  const [ddAPIKey, setDdAPIKey] = useState("");
  const [ddService, setDdService] = useState("openzro");
  const [ddTags, setDdTags] = useState("");

  // Elastic fields
  const [esURL, setEsURL] = useState("");
  const [esIndex, setEsIndex] = useState("openzro-events");
  const [esAPIKey, setEsAPIKey] = useState("");
  const [esUsername, setEsUsername] = useState("");
  const [esPassword, setEsPassword] = useState("");

  const { mutate } = useSWRConfig();
  const apiCreate = useApiCall<ActivityExporter>("/admin/activity-exporters");
  const apiUpdate = useApiCall<ActivityExporter>(
    `/admin/activity-exporters/${existing?.id ?? 0}`,
  );
  const apiValidate = useApiCall<{ ok: boolean }>(
    "/admin/activity-exporters/validate-template",
  );

  useEffect(() => {
    if (!open) return;
    if (existing) {
      setName(existing.name);
      setType(existing.type);
      setEnabled(existing.enabled);
      setTemplate(existing.template ?? "");
      const cfg = existing.config as any;
      switch (existing.type) {
        case "http":
          setHttpURL(cfg?.url ?? "");
          setHttpHeadersJSON("");
          break;
        case "datadog":
          setDdSite(cfg?.site ?? "us1");
          setDdURL(cfg?.url ?? "");
          setDdAPIKey("");
          setDdService(cfg?.service ?? "openzro");
          setDdTags(cfg?.tags ?? "");
          break;
        case "elastic":
          setEsURL(cfg?.url ?? "");
          setEsIndex(cfg?.index ?? "openzro-events");
          setEsAPIKey("");
          setEsUsername("");
          setEsPassword("");
          break;
      }
    } else {
      setName("");
      setType("http");
      setEnabled(true);
      setTemplate("");
      setHttpURL("");
      setHttpHeadersJSON("");
      setDdSite("us1");
      setDdURL("");
      setDdAPIKey("");
      setDdService("openzro");
      setDdTags("");
      setEsURL("");
      setEsIndex("openzro-events");
      setEsAPIKey("");
      setEsUsername("");
      setEsPassword("");
    }
  }, [open, existing]);

  const buildInput = (): ActivityExporterInput => {
    const base: ActivityExporterInput = {
      name,
      type,
      enabled,
      template: template.trim() || undefined,
    };
    if (type === "http") {
      let headers: Record<string, string> | undefined;
      if (httpHeadersJSON.trim()) {
        try {
          headers = JSON.parse(httpHeadersJSON);
        } catch {
          /* validation will surface the error */
        }
      }
      base.http = { url: httpURL, headers };
    } else if (type === "datadog") {
      base.datadog = {
        site: ddSite || undefined,
        url: ddURL || undefined,
        api_key: ddAPIKey || undefined,
        service: ddService || undefined,
        tags: ddTags || undefined,
      };
    } else if (type === "elastic") {
      base.elastic = {
        url: esURL,
        index: esIndex || undefined,
        api_key: esAPIKey || undefined,
        username: esUsername || undefined,
        password: esPassword || undefined,
      };
    }
    return base;
  };

  const validate = (): string | null => {
    if (!name.trim()) return "Name is required";
    if (type === "http") {
      if (!httpURL) return "HTTP URL is required";
      if (httpHeadersJSON.trim()) {
        try {
          const parsed = JSON.parse(httpHeadersJSON);
          if (typeof parsed !== "object" || Array.isArray(parsed)) {
            return "Headers must be a JSON object";
          }
        } catch {
          return "Headers must be valid JSON";
        }
      }
    } else if (type === "datadog") {
      if (!isEdit && !ddAPIKey)
        return "Datadog API key is required on first save";
    } else if (type === "elastic") {
      if (!esURL) return "Elastic URL is required";
      if (!isEdit && !esAPIKey && !esUsername)
        return "Elastic auth required (API key or basic) on first save";
    }
    return null;
  };

  const onSave = async () => {
    const err = validate();
    if (err) {
      notify({
        title: "Cannot save",
        description: err,
        promise: Promise.reject(new Error(err)),
        loadingMessage: "Saving exporter...",
      });
      return;
    }
    setSaving(true);
    try {
      if (isEdit) {
        await apiUpdate.put(buildInput());
      } else {
        await apiCreate.post(buildInput());
      }
      await mutate("/admin/activity-exporters");
      notify({
        title: isEdit ? "Updated" : "Created",
        description: `Activity exporter ${name} saved`,
      });
      setOpen(false);
    } catch {
      // useApiCall surfaces toast
    } finally {
      setSaving(false);
    }
  };

  const onTestTemplate = async () => {
    if (!template.trim()) {
      notify({
        title: "Template",
        description: "Empty template — nothing to test",
        promise: Promise.resolve(),
        loadingMessage: "Validating template...",
      });
      return;
    }
    setValidating(true);
    try {
      await apiValidate.post({ template });
      notify({
        title: "Template",
        description: "Template is valid against a sample event.",
      });
    } catch {
      // error toast already shown
    } finally {
      setValidating(false);
    }
  };

  return (
    <Modal open={open} onOpenChange={setOpen} key={open ? "open" : "closed"}>
      <ModalContent maxWidthClass={"max-w-2xl"} showClose={true}>
        <ModalHeader
          icon={<CableIcon size={19} />}
          title={isEdit ? "Edit activity exporter" : "Add activity exporter"}
          description={
            "Stream audit log events to your SIEM. Credentials are encrypted at rest."
          }
          color={"openzro"}
        />

        <div className={"flex flex-col gap-4 px-8 pb-2"}>
          <div>
            <OzLabel htmlFor="exporter-name">Name</OzLabel>
            <OzInput
              id="exporter-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={"e.g., Datadog Prod"}
            />
          </div>

          <div>
            <OzLabel>Type</OzLabel>
            <OzSelect
              value={type}
              onValueChange={(v) => setType(v as ActivityExporterType)}
              disabled={isEdit}
            >
              <OzSelectTrigger>
                <OzSelectValue />
              </OzSelectTrigger>
              <OzSelectContent>
                <OzSelectItem value="http">HTTP webhook</OzSelectItem>
                <OzSelectItem value="datadog">Datadog Logs Intake</OzSelectItem>
                <OzSelectItem value="elastic">
                  Elasticsearch (ECS)
                </OzSelectItem>
              </OzSelectContent>
            </OzSelect>
            {isEdit && (
              <OzHelpText className="mt-1.5">
                Type cannot be changed after creation.
              </OzHelpText>
            )}
          </div>

          {type === "http" && (
            <>
              <div>
                <OzLabel htmlFor="exporter-http-url">URL</OzLabel>
                <OzInput
                  id="exporter-http-url"
                  value={httpURL}
                  onChange={(e) => setHttpURL(e.target.value)}
                  placeholder={"https://siem.example.com/webhooks/audit"}
                />
              </div>
              <div>
                <OzLabel htmlFor="exporter-http-headers" optional>
                  Headers (JSON)
                </OzLabel>
                <OzTextarea
                  id="exporter-http-headers"
                  rows={3}
                  value={httpHeadersJSON}
                  onChange={(e) => setHttpHeadersJSON(e.target.value)}
                  placeholder={
                    isEdit ? PLACEHOLDER_KEEP : '{"Authorization":"Bearer ..."}'
                  }
                />
                <OzHelpText className="mt-1.5">
                  Authorization tokens go here. Stored encrypted; never
                  returned by the API.
                </OzHelpText>
              </div>
            </>
          )}

          {type === "datadog" && (
            <>
              <div>
                <OzLabel>Site</OzLabel>
                <OzSelect value={ddSite} onValueChange={setDdSite}>
                  <OzSelectTrigger>
                    <OzSelectValue />
                  </OzSelectTrigger>
                  <OzSelectContent>
                    <OzSelectItem value="us1">
                      US1 (datadoghq.com)
                    </OzSelectItem>
                    <OzSelectItem value="us3">US3</OzSelectItem>
                    <OzSelectItem value="us5">US5</OzSelectItem>
                    <OzSelectItem value="eu1">EU1 (datadoghq.eu)</OzSelectItem>
                    <OzSelectItem value="ap1">AP1</OzSelectItem>
                  </OzSelectContent>
                </OzSelect>
                <OzHelpText className="mt-1.5">
                  Pick the site your Datadog org lives on — sending to
                  the wrong site silently 401s.
                </OzHelpText>
              </div>
              <div>
                <OzLabel htmlFor="exporter-dd-key">API key</OzLabel>
                <OzInput
                  id="exporter-dd-key"
                  type="password"
                  value={ddAPIKey}
                  onChange={(e) => setDdAPIKey(e.target.value)}
                  placeholder={isEdit ? PLACEHOLDER_KEEP : "DD-API-KEY"}
                />
              </div>
              <div className={"grid grid-cols-2 gap-3"}>
                <div>
                  <OzLabel htmlFor="exporter-dd-service">Service</OzLabel>
                  <OzInput
                    id="exporter-dd-service"
                    value={ddService}
                    onChange={(e) => setDdService(e.target.value)}
                    placeholder={"openzro"}
                  />
                </div>
                <div>
                  <OzLabel htmlFor="exporter-dd-tags">Tags</OzLabel>
                  <OzInput
                    id="exporter-dd-tags"
                    value={ddTags}
                    onChange={(e) => setDdTags(e.target.value)}
                    placeholder={"env:prod,team:secops"}
                  />
                </div>
              </div>
              <div>
                <OzLabel htmlFor="exporter-dd-url" optional>
                  URL override
                </OzLabel>
                <OzInput
                  id="exporter-dd-url"
                  value={ddURL}
                  onChange={(e) => setDdURL(e.target.value)}
                  placeholder={"https://datadog-proxy.internal"}
                />
                <OzHelpText className="mt-1.5">
                  Only when proxying through an internal log forwarder.
                </OzHelpText>
              </div>
            </>
          )}

          {type === "elastic" && (
            <>
              <div>
                <OzLabel htmlFor="exporter-es-url">URL</OzLabel>
                <OzInput
                  id="exporter-es-url"
                  value={esURL}
                  onChange={(e) => setEsURL(e.target.value)}
                  placeholder={"https://es.example.com:9200"}
                />
              </div>
              <div>
                <OzLabel htmlFor="exporter-es-index">Index</OzLabel>
                <OzInput
                  id="exporter-es-index"
                  value={esIndex}
                  onChange={(e) => setEsIndex(e.target.value)}
                  placeholder={"openzro-events"}
                />
              </div>
              <div className={"grid grid-cols-2 gap-3"}>
                <div>
                  <OzLabel htmlFor="exporter-es-key">API key</OzLabel>
                  <OzInput
                    id="exporter-es-key"
                    type="password"
                    value={esAPIKey}
                    onChange={(e) => setEsAPIKey(e.target.value)}
                    placeholder={isEdit ? PLACEHOLDER_KEEP : "preferred"}
                  />
                </div>
                <div>
                  <OzLabel htmlFor="exporter-es-username">OR Username</OzLabel>
                  <OzInput
                    id="exporter-es-username"
                    value={esUsername}
                    onChange={(e) => setEsUsername(e.target.value)}
                    placeholder={"basic auth fallback"}
                  />
                </div>
              </div>
              {esUsername && (
                <div>
                  <OzLabel htmlFor="exporter-es-password">Password</OzLabel>
                  <OzInput
                    id="exporter-es-password"
                    type="password"
                    value={esPassword}
                    onChange={(e) => setEsPassword(e.target.value)}
                    placeholder={isEdit ? PLACEHOLDER_KEEP : ""}
                  />
                </div>
              )}
            </>
          )}

          <div>
            <OzLabel htmlFor="exporter-template" optional>
              Custom payload template
            </OzLabel>
            <OzTextarea
              id="exporter-template"
              rows={5}
              value={template}
              onChange={(e) => setTemplate(e.target.value)}
              placeholder={
                '{{ json (dict "ts" (rfc3339 .Timestamp) "user" .InitiatorEmail "act" .Activity) }}'
              }
              className={"font-mono text-xs"}
            />
            <OzHelpText className="mt-1.5">
              Go text/template syntax. Bound to the activity event as{" "}
              <code className={"font-mono text-xs"}>.</code> with helpers{" "}
              <code className={"font-mono text-xs"}>json</code>,{" "}
              <code className={"font-mono text-xs"}>dict</code>,{" "}
              <code className={"font-mono text-xs"}>rfc3339</code>,{" "}
              <code className={"font-mono text-xs"}>default</code>,{" "}
              <code className={"font-mono text-xs"}>meta</code>. Empty =
              ship the exporter&apos;s native default payload.
            </OzHelpText>
            <div className={"mt-2"}>
              <OzButton
                variant={"default"}
                onClick={onTestTemplate}
                disabled={validating || !template.trim()}
              >
                {validating ? "Validating…" : "Validate template"}
              </OzButton>
            </div>
          </div>

          <p className={"text-xs text-oz2-text-muted"}>
            Events are batched and shipped asynchronously — a slow
            destination never blocks the API path.
          </p>
        </div>

        <ModalFooter>
          <ModalClose asChild>
            <OzButton variant={"default"} disabled={saving}>
              Cancel
            </OzButton>
          </ModalClose>
          <OzButton variant={"primary"} onClick={onSave} disabled={saving}>
            {saving ? "Saving…" : isEdit ? "Save changes" : "Create exporter"}
          </OzButton>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
