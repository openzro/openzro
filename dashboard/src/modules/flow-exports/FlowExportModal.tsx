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
import OzLabel from "@/components/v2/OzLabel";
import {
  OzSelect,
  OzSelectContent,
  OzSelectItem,
  OzSelectTrigger,
  OzSelectValue,
} from "@/components/v2/OzSelect";
import OzTextarea from "@/components/v2/OzTextarea";
import {
  FlowExport,
  FlowExportInput,
  FlowExportType,
} from "@/interfaces/FlowExport";

type Props = {
  open: boolean;
  setOpen: (open: boolean) => void;
  // When editing, the existing row. Null/undefined for create.
  existing?: FlowExport | null;
};

// FlowExportModal handles both create and edit. When editing, secret
// fields render with a "Leave blank to keep existing" placeholder —
// the API does not return secrets back, so they cannot be pre-filled.
export default function FlowExportModal({
  open,
  setOpen,
  existing,
}: Readonly<Props>) {
  const isEdit = !!existing;
  const [name, setName] = useState("");
  const [type, setType] = useState<FlowExportType>("elastic");
  const [saving, setSaving] = useState(false);

  // Elastic fields
  const [elasticURL, setElasticURL] = useState("");
  const [elasticIndex, setElasticIndex] = useState("");
  const [elasticAPIKey, setElasticAPIKey] = useState("");
  const [elasticUsername, setElasticUsername] = useState("");
  const [elasticPassword, setElasticPassword] = useState("");

  // S3 fields
  const [s3Bucket, setS3Bucket] = useState("");
  const [s3Region, setS3Region] = useState("");
  const [s3Endpoint, setS3Endpoint] = useState("");
  const [s3AccessKey, setS3AccessKey] = useState("");
  const [s3SecretKey, setS3SecretKey] = useState("");
  const [s3Prefix, setS3Prefix] = useState("");

  // HTTP fields
  const [httpURL, setHTTPURL] = useState("");

  // Datadog fields
  const [ddSite, setDdSite] = useState("us1");
  const [ddURL, setDdURL] = useState("");
  const [ddAPIKey, setDdAPIKey] = useState("");
  const [ddService, setDdService] = useState("openzro-flow");
  const [ddTags, setDdTags] = useState("");

  // GCS native fields
  const [gcsBucket, setGcsBucket] = useState("");
  const [gcsPrefix, setGcsPrefix] = useState("");
  const [gcsAuthMode, setGcsAuthMode] = useState<"adc" | "file" | "json">(
    "adc",
  );
  const [gcsCredFile, setGcsCredFile] = useState("");
  const [gcsCredJSON, setGcsCredJSON] = useState("");
  const [gcsProjectID, setGcsProjectID] = useState("");

  const { mutate } = useSWRConfig();
  const apiCreate = useApiCall<FlowExport>("/admin/flow-exports");
  const apiUpdate = useApiCall<FlowExport>(
    `/admin/flow-exports/${existing?.id ?? 0}`,
  );

  // Reset form on open / when target changes.
  useEffect(() => {
    if (!open) return;
    if (existing) {
      setName(existing.name);
      setType(existing.type);
      const cfg = existing.config as any;
      switch (existing.type) {
        case "elastic":
          setElasticURL(cfg?.url ?? "");
          setElasticIndex(cfg?.index ?? "");
          setElasticAPIKey("");
          setElasticUsername("");
          setElasticPassword("");
          break;
        case "s3":
          setS3Bucket(cfg?.bucket ?? "");
          setS3Region(cfg?.region ?? "");
          setS3Endpoint(cfg?.endpoint ?? "");
          setS3AccessKey("");
          setS3SecretKey("");
          setS3Prefix(cfg?.prefix ?? "");
          break;
        case "http":
          setHTTPURL(cfg?.url ?? "");
          break;
        case "datadog":
          setDdSite(cfg?.site ?? "us1");
          setDdURL(cfg?.url ?? "");
          setDdAPIKey("");
          setDdService(cfg?.service ?? "openzro-flow");
          setDdTags(cfg?.tags ?? "");
          break;
        case "gcs":
          setGcsBucket(cfg?.bucket ?? "");
          setGcsPrefix(cfg?.prefix ?? "");
          setGcsAuthMode(
            cfg?.auth_mode === "file"
              ? "file"
              : cfg?.auth_mode === "inline-json"
                ? "json"
                : "adc",
          );
          setGcsCredFile("");
          setGcsCredJSON("");
          setGcsProjectID(cfg?.project_id ?? "");
          break;
      }
    } else {
      setName("");
      setType("elastic");
      setElasticURL("");
      setElasticIndex("");
      setElasticAPIKey("");
      setElasticUsername("");
      setElasticPassword("");
      setS3Bucket("");
      setS3Region("");
      setS3Endpoint("");
      setS3AccessKey("");
      setS3SecretKey("");
      setS3Prefix("");
      setHTTPURL("");
      setDdSite("us1");
      setDdURL("");
      setDdAPIKey("");
      setDdService("openzro-flow");
      setDdTags("");
      setGcsBucket("");
      setGcsPrefix("");
      setGcsAuthMode("adc");
      setGcsCredFile("");
      setGcsCredJSON("");
      setGcsProjectID("");
    }
  }, [open, existing]);

  const buildInput = (): FlowExportInput => {
    const base: FlowExportInput = { name, type, enabled: true };
    if (type === "elastic") {
      base.elastic = {
        url: elasticURL,
        index: elasticIndex || undefined,
        api_key: elasticAPIKey || undefined,
        username: elasticUsername || undefined,
        password: elasticPassword || undefined,
      };
    } else if (type === "s3") {
      base.s3 = {
        bucket: s3Bucket,
        region: s3Region || undefined,
        endpoint: s3Endpoint || undefined,
        access_key: s3AccessKey || undefined,
        secret_key: s3SecretKey || undefined,
        prefix: s3Prefix || undefined,
      };
    } else if (type === "http") {
      base.http = { url: httpURL };
    } else if (type === "datadog") {
      base.datadog = {
        site: ddSite || undefined,
        url: ddURL || undefined,
        api_key: ddAPIKey || undefined,
        service: ddService || undefined,
        tags: ddTags || undefined,
      };
    } else if (type === "gcs") {
      base.gcs = {
        bucket: gcsBucket,
        prefix: gcsPrefix || undefined,
        project_id: gcsProjectID || undefined,
        credentials_file: gcsAuthMode === "file" ? gcsCredFile || undefined : undefined,
        credentials_json: gcsAuthMode === "json" ? gcsCredJSON || undefined : undefined,
      };
    }
    return base;
  };

  const validate = (): string | null => {
    if (!name.trim()) return "Name is required";
    if (type === "elastic") {
      if (!elasticURL.trim()) return "Elastic URL is required";
      if (!isEdit && !elasticAPIKey && !elasticUsername) {
        return "Provide either an API key or a username/password";
      }
    } else if (type === "s3") {
      if (!s3Bucket.trim()) return "S3 bucket is required";
    } else if (type === "http") {
      if (!httpURL.trim()) return "HTTP URL is required";
    } else if (type === "datadog") {
      if (!isEdit && !ddAPIKey.trim()) {
        return "Datadog API key is required on first save";
      }
    } else if (type === "gcs") {
      if (!gcsBucket.trim()) return "GCS bucket is required";
      if (gcsAuthMode === "json" && !isEdit && !gcsCredJSON.trim()) {
        return "Inline service-account JSON is required on first save";
      }
      if (gcsAuthMode === "file" && !isEdit && !gcsCredFile.trim()) {
        return "Credentials file path is required on first save";
      }
    }
    return null;
  };

  const onSave = async () => {
    const err = validate();
    if (err) {
      notify({ title: "Validation error", description: err });
      return;
    }
    setSaving(true);
    try {
      if (isEdit) {
        await apiUpdate.put(buildInput());
      } else {
        await apiCreate.post(buildInput());
      }
      await mutate("/admin/flow-exports");
      setOpen(false);
      notify({
        title: isEdit ? "Updated" : "Created",
        description: `Flow export "${name}" saved.`,
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal open={open} onOpenChange={setOpen}>
      <ModalContent maxWidthClass="max-w-2xl">
        <ModalHeader
          icon={<CableIcon size={20} />}
          title={isEdit ? "Edit flow export" : "Add flow export"}
          description={
            isEdit
              ? "Update destination details. Leave secret fields blank to keep the current value."
              : "Stream traffic events to Elasticsearch, S3, or any HTTP webhook."
          }
          truncate
        />

        <div className="px-8 pt-3 pb-6 grid gap-4">
          <div>
            <OzLabel htmlFor="flow-export-name">Name</OzLabel>
            <OzInput
              id="flow-export-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="prod-elastic"
            />
          </div>

          <div>
            <OzLabel>Type</OzLabel>
            <OzSelect
              value={type}
              onValueChange={(v) => setType(v as FlowExportType)}
              disabled={isEdit}
            >
              <OzSelectTrigger>
                <OzSelectValue />
              </OzSelectTrigger>
              <OzSelectContent>
                <OzSelectItem value="elastic">
                  Elasticsearch (SIEM)
                </OzSelectItem>
                <OzSelectItem value="datadog">
                  Datadog Logs Intake (SIEM/NPM)
                </OzSelectItem>
                <OzSelectItem value="s3">
                  S3 / R2 / B2 / GCS Interop / MinIO (cold archive)
                </OzSelectItem>
                <OzSelectItem value="gcs">
                  Google Cloud Storage native (cold archive)
                </OzSelectItem>
                <OzSelectItem value="http">HTTP webhook</OzSelectItem>
              </OzSelectContent>
            </OzSelect>
          </div>

          {type === "elastic" && (
            <ElasticForm
              url={elasticURL}
              setURL={setElasticURL}
              index={elasticIndex}
              setIndex={setElasticIndex}
              apiKey={elasticAPIKey}
              setAPIKey={setElasticAPIKey}
              username={elasticUsername}
              setUsername={setElasticUsername}
              password={elasticPassword}
              setPassword={setElasticPassword}
              isEdit={isEdit}
            />
          )}

          {type === "s3" && (
            <S3Form
              bucket={s3Bucket}
              setBucket={setS3Bucket}
              region={s3Region}
              setRegion={setS3Region}
              endpoint={s3Endpoint}
              setEndpoint={setS3Endpoint}
              accessKey={s3AccessKey}
              setAccessKey={setS3AccessKey}
              secretKey={s3SecretKey}
              setSecretKey={setS3SecretKey}
              prefix={s3Prefix}
              setPrefix={setS3Prefix}
              isEdit={isEdit}
            />
          )}

          {type === "http" && (
            <div>
              <OzLabel htmlFor="flow-http-url">URL</OzLabel>
              <OzInput
                id="flow-http-url"
                value={httpURL}
                onChange={(e) => setHTTPURL(e.target.value)}
                placeholder="https://example.com/ingest"
              />
            </div>
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
                <p className="text-xs text-oz2-text-muted mt-1.5">
                  Pick the site your Datadog org lives on — sending to
                  the wrong site silently 401s.
                </p>
              </div>
              <div>
                <OzLabel htmlFor="flow-dd-key">API key</OzLabel>
                <OzInput
                  id="flow-dd-key"
                  type="password"
                  value={ddAPIKey}
                  onChange={(e) => setDdAPIKey(e.target.value)}
                  placeholder={isEdit ? "(unchanged)" : "DD-API-KEY"}
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <OzLabel htmlFor="flow-dd-service">Service</OzLabel>
                  <OzInput
                    id="flow-dd-service"
                    value={ddService}
                    onChange={(e) => setDdService(e.target.value)}
                    placeholder="openzro-flow"
                  />
                </div>
                <div>
                  <OzLabel htmlFor="flow-dd-tags">Tags</OzLabel>
                  <OzInput
                    id="flow-dd-tags"
                    value={ddTags}
                    onChange={(e) => setDdTags(e.target.value)}
                    placeholder="env:prod,team:secops"
                  />
                </div>
              </div>
              <div>
                <OzLabel htmlFor="flow-dd-url" optional>
                  URL override
                </OzLabel>
                <OzInput
                  id="flow-dd-url"
                  value={ddURL}
                  onChange={(e) => setDdURL(e.target.value)}
                  placeholder="https://datadog-proxy.internal"
                />
                <p className="text-xs text-oz2-text-muted mt-1.5">
                  Only when proxying through an internal log forwarder.
                </p>
              </div>
            </>
          )}

          {type === "gcs" && (
            <>
              <div>
                <OzLabel htmlFor="flow-gcs-bucket">Bucket</OzLabel>
                <OzInput
                  id="flow-gcs-bucket"
                  value={gcsBucket}
                  onChange={(e) => setGcsBucket(e.target.value)}
                  placeholder="openzro-flow-archive"
                />
              </div>
              <div>
                <OzLabel htmlFor="flow-gcs-prefix" optional>
                  Prefix
                </OzLabel>
                <OzInput
                  id="flow-gcs-prefix"
                  value={gcsPrefix}
                  onChange={(e) => setGcsPrefix(e.target.value)}
                  placeholder="openzro/prod"
                />
              </div>
              <div>
                <OzLabel htmlFor="flow-gcs-project" optional>
                  Project ID
                </OzLabel>
                <OzInput
                  id="flow-gcs-project"
                  value={gcsProjectID}
                  onChange={(e) => setGcsProjectID(e.target.value)}
                  placeholder="my-gcp-project"
                />
              </div>
              <div>
                <OzLabel>Authentication</OzLabel>
                <OzSelect
                  value={gcsAuthMode}
                  onValueChange={(v) =>
                    setGcsAuthMode(v as "adc" | "file" | "json")
                  }
                >
                  <OzSelectTrigger>
                    <OzSelectValue />
                  </OzSelectTrigger>
                  <OzSelectContent>
                    <OzSelectItem value="adc">
                      Application Default Credentials (Workload Identity, GKE,
                      Cloud Run, gcloud)
                    </OzSelectItem>
                    <OzSelectItem value="file">
                      Service Account JSON (file path)
                    </OzSelectItem>
                    <OzSelectItem value="json">
                      Service Account JSON (inline)
                    </OzSelectItem>
                  </OzSelectContent>
                </OzSelect>
                <p className="text-xs text-oz2-text-muted mt-1.5">
                  ADC is the recommended posture inside GCP — no
                  credential files in the container, IAM bound to the
                  workload identity. Self-host outside GCP uses the
                  file or inline JSON.
                </p>
              </div>
              {gcsAuthMode === "file" && (
                <div>
                  <OzLabel htmlFor="flow-gcs-cred-file">
                    Credentials file path
                  </OzLabel>
                  <OzInput
                    id="flow-gcs-cred-file"
                    value={gcsCredFile}
                    onChange={(e) => setGcsCredFile(e.target.value)}
                    placeholder={
                      isEdit
                        ? "(unchanged)"
                        : "/etc/openzro/gcs-service-account.json"
                    }
                  />
                </div>
              )}
              {gcsAuthMode === "json" && (
                <div>
                  <OzLabel htmlFor="flow-gcs-cred-json">
                    Service account JSON
                  </OzLabel>
                  <OzTextarea
                    id="flow-gcs-cred-json"
                    value={gcsCredJSON}
                    onChange={(e) => setGcsCredJSON(e.target.value)}
                    placeholder={
                      isEdit ? "(unchanged)" : '{"type":"service_account",...}'
                    }
                    rows={6}
                    className="font-mono text-xs"
                  />
                </div>
              )}
            </>
          )}
        </div>

        <ModalFooter className="items-center gap-3">
          <ModalClose asChild>
            <OzButton variant="default">Cancel</OzButton>
          </ModalClose>
          <OzButton variant="primary" onClick={onSave} disabled={saving}>
            {saving ? "Saving..." : isEdit ? "Save changes" : "Create"}
          </OzButton>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}

function ElasticForm({
  url,
  setURL,
  index,
  setIndex,
  apiKey,
  setAPIKey,
  username,
  setUsername,
  password,
  setPassword,
  isEdit,
}: {
  url: string;
  setURL: (v: string) => void;
  index: string;
  setIndex: (v: string) => void;
  apiKey: string;
  setAPIKey: (v: string) => void;
  username: string;
  setUsername: (v: string) => void;
  password: string;
  setPassword: (v: string) => void;
  isEdit: boolean;
}) {
  return (
    <>
      <div>
        <OzLabel htmlFor="flow-elastic-url">Cluster URL</OzLabel>
        <OzInput
          id="flow-elastic-url"
          value={url}
          onChange={(e) => setURL(e.target.value)}
          placeholder="https://es.example.com:9200"
        />
      </div>
      <div>
        <OzLabel htmlFor="flow-elastic-index" optional>
          Index
        </OzLabel>
        <OzInput
          id="flow-elastic-index"
          value={index}
          onChange={(e) => setIndex(e.target.value)}
          placeholder="openzro-flow"
        />
      </div>
      <p className="text-xs text-oz2-text-muted">
        Provide an API key (preferred) OR a username + password.
        {isEdit && " Leave blank to keep the existing credential."}
      </p>
      <div>
        <OzLabel htmlFor="flow-elastic-key">API key</OzLabel>
        <OzInput
          id="flow-elastic-key"
          type="password"
          value={apiKey}
          onChange={(e) => setAPIKey(e.target.value)}
          placeholder={isEdit ? "(unchanged)" : ""}
        />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <OzLabel htmlFor="flow-elastic-user">Username</OzLabel>
          <OzInput
            id="flow-elastic-user"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </div>
        <div>
          <OzLabel htmlFor="flow-elastic-pass">Password</OzLabel>
          <OzInput
            id="flow-elastic-pass"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={isEdit ? "(unchanged)" : ""}
          />
        </div>
      </div>
    </>
  );
}

// Provider presets fill the Endpoint (and sometimes Region) for the
// common S3-compatible services. The bucket-level settings still come
// from the user. AWS S3 is the default — it leaves Endpoint empty so
// the SDK uses its built-in routing.
//
// GCS works via Cloud Storage's "Interoperability" mode: enable it in
// the GCP Console, mint an HMAC key (access_key + secret_key), point
// the Endpoint here. Native service-account JSON auth would need the
// google-cloud-go SDK; for now interop covers it.
const s3Presets = [
  { id: "aws", label: "AWS S3", endpoint: "" },
  {
    id: "r2",
    label: "Cloudflare R2",
    endpoint: "https://<account>.r2.cloudflarestorage.com",
  },
  {
    id: "b2",
    label: "Backblaze B2",
    endpoint: "https://s3.<region>.backblazeb2.com",
  },
  {
    id: "gcs",
    label: "Google Cloud Storage (Interop)",
    endpoint: "https://storage.googleapis.com",
  },
  { id: "minio", label: "MinIO / self-hosted", endpoint: "https://minio.example.com" },
  { id: "custom", label: "Custom", endpoint: "" },
];

function S3Form({
  bucket,
  setBucket,
  region,
  setRegion,
  endpoint,
  setEndpoint,
  accessKey,
  setAccessKey,
  secretKey,
  setSecretKey,
  prefix,
  setPrefix,
  isEdit,
}: {
  bucket: string;
  setBucket: (v: string) => void;
  region: string;
  setRegion: (v: string) => void;
  endpoint: string;
  setEndpoint: (v: string) => void;
  accessKey: string;
  setAccessKey: (v: string) => void;
  secretKey: string;
  setSecretKey: (v: string) => void;
  prefix: string;
  setPrefix: (v: string) => void;
  isEdit: boolean;
}) {
  const [preset, setPreset] = useState("aws");
  const onPresetChange = (id: string) => {
    setPreset(id);
    const next = s3Presets.find((p) => p.id === id);
    if (!next) return;
    setEndpoint(next.endpoint);
    if (id === "gcs" && !region) setRegion("auto");
    if (id === "r2" && !region) setRegion("auto");
  };

  return (
    <>
      <div>
        <OzLabel>Provider</OzLabel>
        <OzSelect value={preset} onValueChange={onPresetChange}>
          <OzSelectTrigger>
            <OzSelectValue />
          </OzSelectTrigger>
          <OzSelectContent>
            {s3Presets.map((p) => (
              <OzSelectItem key={p.id} value={p.id}>
                {p.label}
              </OzSelectItem>
            ))}
          </OzSelectContent>
        </OzSelect>
        <p className="text-xs text-oz2-text-muted mt-1.5">
          GCS works via Cloud Storage&apos;s Interoperability mode —
          enable it in the GCP console and mint an HMAC key.
        </p>
      </div>
      <div>
        <OzLabel htmlFor="flow-s3-bucket">Bucket</OzLabel>
        <OzInput
          id="flow-s3-bucket"
          value={bucket}
          onChange={(e) => setBucket(e.target.value)}
          placeholder="openzro-flow-archive"
        />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <OzLabel htmlFor="flow-s3-region">Region</OzLabel>
          <OzInput
            id="flow-s3-region"
            value={region}
            onChange={(e) => setRegion(e.target.value)}
            placeholder="us-east-1"
          />
        </div>
        <div>
          <OzLabel htmlFor="flow-s3-endpoint" optional>
            Endpoint (AWS uses default)
          </OzLabel>
          <OzInput
            id="flow-s3-endpoint"
            value={endpoint}
            onChange={(e) => setEndpoint(e.target.value)}
            placeholder="https://storage.googleapis.com"
          />
        </div>
      </div>
      <p className="text-xs text-oz2-text-muted">
        Provide HMAC-style credentials, or leave blank for AWS to use
        the SDK default credential chain (env vars, profile, IAM role).
        For GCS Interop and Cloudflare R2, the access/secret keys are
        required.
        {isEdit && " Leave blank to keep the existing values."}
      </p>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <OzLabel htmlFor="flow-s3-access">Access key</OzLabel>
          <OzInput
            id="flow-s3-access"
            value={accessKey}
            onChange={(e) => setAccessKey(e.target.value)}
            placeholder={isEdit ? "(unchanged)" : ""}
          />
        </div>
        <div>
          <OzLabel htmlFor="flow-s3-secret">Secret key</OzLabel>
          <OzInput
            id="flow-s3-secret"
            type="password"
            value={secretKey}
            onChange={(e) => setSecretKey(e.target.value)}
            placeholder={isEdit ? "(unchanged)" : ""}
          />
        </div>
      </div>
      <div>
        <OzLabel htmlFor="flow-s3-prefix" optional>
          Prefix
        </OzLabel>
        <OzInput
          id="flow-s3-prefix"
          value={prefix}
          onChange={(e) => setPrefix(e.target.value)}
          placeholder="openzro/prod"
        />
      </div>
    </>
  );
}
