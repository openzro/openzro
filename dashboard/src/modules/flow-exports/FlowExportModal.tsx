"use client";

import Button from "@components/Button";
import { Input } from "@components/Input";
import { Label } from "@components/Label";
import {
  Modal,
  ModalClose,
  ModalContent,
  ModalFooter,
} from "@components/modal/Modal";
import ModalHeader from "@components/modal/ModalHeader";
import { notify } from "@components/Notification";
import Paragraph from "@components/Paragraph";
import { useApiCall } from "@utils/api";
import { CableIcon } from "lucide-react";
import React, { useEffect, useState } from "react";
import { useSWRConfig } from "swr";
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
            <Label>Name</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="prod-elastic"
            />
          </div>

          <div>
            <Label>Type</Label>
            <select
              value={type}
              disabled={isEdit}
              onChange={(e) => setType(e.target.value as FlowExportType)}
              className="w-full rounded-md border border-nb-gray-700 bg-nb-gray-940 px-3 py-2 text-sm"
            >
              <option value="elastic">Elasticsearch (SIEM)</option>
              <option value="s3">
                S3 / R2 / B2 / GCS / MinIO (cold archive)
              </option>
              <option value="http">HTTP webhook</option>
            </select>
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
              <Label>URL</Label>
              <Input
                value={httpURL}
                onChange={(e) => setHTTPURL(e.target.value)}
                placeholder="https://example.com/ingest"
              />
            </div>
          )}
        </div>

        <ModalFooter className="items-center gap-3">
          <ModalClose asChild>
            <Button variant="secondary">Cancel</Button>
          </ModalClose>
          <Button variant="primary" onClick={onSave} disabled={saving}>
            {saving ? "Saving..." : isEdit ? "Save changes" : "Create"}
          </Button>
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
        <Label>Cluster URL</Label>
        <Input
          value={url}
          onChange={(e) => setURL(e.target.value)}
          placeholder="https://es.example.com:9200"
        />
      </div>
      <div>
        <Label>Index (optional)</Label>
        <Input
          value={index}
          onChange={(e) => setIndex(e.target.value)}
          placeholder="openzro-flow"
        />
      </div>
      <Paragraph className="text-xs text-nb-gray-300">
        Provide an API key (preferred) OR a username + password.
        {isEdit && " Leave blank to keep the existing credential."}
      </Paragraph>
      <div>
        <Label>API key</Label>
        <Input
          type="password"
          value={apiKey}
          onChange={(e) => setAPIKey(e.target.value)}
          placeholder={isEdit ? "(unchanged)" : ""}
        />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <Label>Username</Label>
          <Input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </div>
        <div>
          <Label>Password</Label>
          <Input
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
  const onPresetChange = (id: string) => {
    const preset = s3Presets.find((p) => p.id === id);
    if (!preset) return;
    setEndpoint(preset.endpoint);
    if (id === "gcs" && !region) setRegion("auto");
    if (id === "r2" && !region) setRegion("auto");
  };

  return (
    <>
      <div>
        <Label>Provider</Label>
        <select
          onChange={(e) => onPresetChange(e.target.value)}
          className="w-full rounded-md border border-nb-gray-700 bg-nb-gray-940 px-3 py-2 text-sm"
          defaultValue="aws"
        >
          {s3Presets.map((p) => (
            <option key={p.id} value={p.id}>
              {p.label}
            </option>
          ))}
        </select>
        <Paragraph className="text-xs text-nb-gray-400 mt-1">
          GCS works via Cloud Storage&apos;s Interoperability mode —
          enable it in the GCP console and mint an HMAC key.
        </Paragraph>
      </div>
      <div>
        <Label>Bucket</Label>
        <Input
          value={bucket}
          onChange={(e) => setBucket(e.target.value)}
          placeholder="openzro-flow-archive"
        />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <Label>Region</Label>
          <Input
            value={region}
            onChange={(e) => setRegion(e.target.value)}
            placeholder="us-east-1"
          />
        </div>
        <div>
          <Label>Endpoint (optional for AWS)</Label>
          <Input
            value={endpoint}
            onChange={(e) => setEndpoint(e.target.value)}
            placeholder="https://storage.googleapis.com"
          />
        </div>
      </div>
      <Paragraph className="text-xs text-nb-gray-300">
        Provide HMAC-style credentials, or leave blank for AWS to use
        the SDK default credential chain (env vars, profile, IAM role).
        For GCS Interop and Cloudflare R2, the access/secret keys are
        required.
        {isEdit && " Leave blank to keep the existing values."}
      </Paragraph>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <Label>Access key</Label>
          <Input
            value={accessKey}
            onChange={(e) => setAccessKey(e.target.value)}
            placeholder={isEdit ? "(unchanged)" : ""}
          />
        </div>
        <div>
          <Label>Secret key</Label>
          <Input
            type="password"
            value={secretKey}
            onChange={(e) => setSecretKey(e.target.value)}
            placeholder={isEdit ? "(unchanged)" : ""}
          />
        </div>
      </div>
      <div>
        <Label>Prefix (optional)</Label>
        <Input
          value={prefix}
          onChange={(e) => setPrefix(e.target.value)}
          placeholder="openzro/prod"
        />
      </div>
    </>
  );
}
