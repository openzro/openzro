import { ModalClose, ModalFooter } from "@components/modal/Modal";
import { RadioGroup, RadioGroupItem } from "@components/RadioGroup";
import { CitySelector } from "@components/ui/CitySelector";
import { CountrySelector } from "@components/ui/CountrySelector";
import { isEmpty, uniqueId } from "lodash";
import {
  ExternalLinkIcon,
  FlagIcon,
  InfoIcon,
  MinusCircleIcon,
  PlusCircle,
  ShieldCheck,
  ShieldXIcon,
} from "lucide-react";
import * as React from "react";
import { useState } from "react";
import OzButton from "@/components/v2/OzButton";
import OzLabel, { OzHelpText } from "@/components/v2/OzLabel";
import { useCountries } from "@/contexts/CountryProvider";
import { GeoLocation, GeoLocationCheck } from "@/interfaces/PostureCheck";
import { PostureCheckCard } from "@/modules/posture-checks/ui/PostureCheckCard";

type Props = {
  value?: GeoLocationCheck;
  onChange: (value: GeoLocationCheck | undefined) => void;
  disabled?: boolean;
};

export const PostureCheckGeoLocation = ({
  value,
  onChange,
  disabled,
}: Props) => {
  const [open, setOpen] = useState(false);

  return (
    <PostureCheckCard
      open={open}
      setOpen={setOpen}
      icon={<FlagIcon size={16} />}
      title={"Country & Region"}
      description={
        "Restrict access in your network based on country or region."
      }
      iconClass={"bg-gradient-to-tr from-indigo-500 to-indigo-400"}
      modalWidthClass={"max-w-2xl"}
      active={value ? value?.locations?.length > 0 : false}
      onReset={() => onChange(undefined)}
      license={
        <div className={"text-xs max-w-xs"}>
          This check includes GeoLite2 data created by MaxMind, available from{" "}
          <a
            href={"https://www.maxmind.com"}
            target={"_blank"}
            rel="noopener noreferrer"
            className="text-oz2-acc-text underline-offset-2 hover:underline"
          >
            https://www.maxmind.com
          </a>
        </div>
      }
    >
      <CheckContent
        value={value}
        onChange={(v) => {
          onChange(v);
          setOpen(false);
        }}
        disabled={disabled}
      />
    </PostureCheckCard>
  );
};

const CheckContent = ({ value, onChange, disabled }: Props) => {
  const { countries, isLoading: countriesLoading } = useCountries();
  // The check can only be configured when the management server has
  // a GeoLite2 database staged or auto-updates enabled. Until then
  // /locations/countries returns []; the country/city dropdowns
  // would be empty and "Add Location" would create useless rows.
  // We surface that state explicitly instead of letting the user
  // fall into the empty-dropdown trap.
  const geoUnavailable =
    !countriesLoading && (!countries || countries.length === 0);

  const [allowDenyLocation, setAllowDenyLocation] = useState<string>(
    value?.action ? value.action : "allow",
  );
  const [locations, setLocations] = useState<GeoLocation[]>(
    value?.locations.map((l) => {
      return {
        id: uniqueId("location"),
        country_code: l.country_code,
        city_name: l.city_name || "",
      };
    }) || [],
  );

  const updateLocation = (id: string, location: GeoLocation) => {
    const find = locations.find((l) => l.id === id);
    if (find) {
      Object.assign(find, location);
      setLocations([...locations]);
    }
  };

  const removeLocation = (id: string) => {
    setLocations(locations.filter((l) => l.id !== id));
  };

  const addLocation = () => {
    setLocations([
      ...locations,
      { id: uniqueId("location"), country_code: "AF", city_name: "" },
    ]);
  };

  return (
    <>
      <div className={"flex flex-col px-8 gap-2 pb-6"}>
        <div className={"flex justify-between items-start gap-10 mt-2"}>
          <div>
            <OzLabel>Allow or Block Location</OzLabel>
            <OzHelpText className="mt-1">
              Choose whether you want to allow or block access from specific
              countries or regions
            </OzHelpText>
          </div>
          <RadioGroup value={allowDenyLocation} onChange={setAllowDenyLocation}>
            <RadioGroupItem value={"allow"} variant={"green"}>
              <ShieldCheck size={16} />
              Allow
            </RadioGroupItem>
            <RadioGroupItem value={"deny"} variant={"red"}>
              <ShieldXIcon size={16} />
              Block
            </RadioGroupItem>
          </RadioGroup>
        </div>
        {geoUnavailable && (
          <div
            className={
              "flex gap-2 items-start text-xs px-3 py-2 rounded-oz2-input border border-oz2-border-soft bg-oz2-bg-sunken text-oz2-text-muted"
            }
          >
            <InfoIcon size={14} className={"mt-0.5 shrink-0"} />
            <div>
              The geolocation database isn&rsquo;t configured on the management
              server, so country and city lists are empty. Stage a GeoLite2
              MaxMind mmdb in the management <code>datadir</code> or run with{" "}
              <code>--disable-geolite-update=false</code> to auto-fetch.{" "}
              <a
                href={
                  "https://docs.openzro.io/how-to/manage-posture-checks#geolocation-check"
                }
                target={"_blank"}
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
              >
                Setup guide
                <ExternalLinkIcon size={12} />
              </a>
            </div>
          </div>
        )}
        {!geoUnavailable && locations.length > 0 && (
          <div className={"mb-2 flex flex-col gap-2 w-full "}>
            {locations.map((location) => {
              return (
                <div key={location.id} className={"flex gap-2 items-start"}>
                  <CountrySelector
                    value={location.country_code}
                    onChange={(value) => {
                      updateLocation(location.id, {
                        ...location,
                        country_code: value,
                      });
                    }}
                  />
                  {location.country_code && (
                    <CitySelector
                      value={location.city_name || ""}
                      onChange={(value) => {
                        updateLocation(location.id, {
                          ...location,
                          city_name: value,
                        });
                      }}
                      country={location.country_code}
                    />
                  )}

                  <button
                    type="button"
                    onClick={() => removeLocation(location.id)}
                    className="grid h-[34px] w-[34px] shrink-0 place-items-center rounded-oz2-input border border-oz2-border bg-oz2-surface text-oz2-text-2 transition-colors hover:bg-oz2-hover hover:border-oz2-border-strong"
                  >
                    <MinusCircleIcon size={15} />
                  </button>
                </div>
              );
            })}
          </div>
        )}
        <button
          type="button"
          disabled={
            allowDenyLocation == "all" || disabled || geoUnavailable
          }
          onClick={addLocation}
          className="inline-flex h-[34px] items-center justify-center gap-2 rounded-oz2-input border border-dashed border-oz2-border-strong bg-transparent px-3 text-[13px] font-medium text-oz2-text-muted transition-colors hover:border-oz2-acc hover:bg-oz2-acc-soft/50 hover:text-oz2-acc-text disabled:cursor-not-allowed disabled:opacity-50"
        >
          <PlusCircle size={16} />
          Add Location
        </button>
      </div>

      <ModalFooter className={"items-center"}>
        <div className={"w-full"}>
          <p className={"text-sm mt-auto text-oz2-text-muted"}>
            Learn more about{" "}
            <a
              href={
                "https://docs.openzro.io/how-to/manage-posture-checks#geolocation-check"
              }
              target={"_blank"}
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-oz2-acc-text underline-offset-2 hover:underline"
            >
              Country & Region Check
              <ExternalLinkIcon size={12} />
            </a>
          </p>
        </div>
        <div className={"flex gap-3 w-full justify-end"}>
          <ModalClose asChild={true}>
            <OzButton variant={"default"}>Cancel</OzButton>
          </ModalClose>
          <OzButton
            variant={"primary"}
            onClick={() => {
              if (isEmpty(locations)) {
                onChange(undefined);
              } else {
                onChange({
                  action: allowDenyLocation as "allow" | "deny",
                  locations: locations,
                });
              }
            }}
            disabled={disabled}
          >
            Save
          </OzButton>
        </div>
      </ModalFooter>
    </>
  );
};
