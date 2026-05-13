import {
  GeoLocation,
  GeoLocationCheck,
  OperatingSystemVersionCheck,
  ScheduleCheck,
} from "@/interfaces/PostureCheck";

const HHMM = /^([01]\d|2[0-3]):[0-5]\d$/;

export const validateOSCheck = (osCheck?: OperatingSystemVersionCheck) => {
  if (!osCheck) return;
  const os = osCheck;
  if (os.darwin && os.darwin.min_version == "") os.darwin.min_version = "0";
  if (os.android && os.android.min_version == "") os.android.min_version = "0";
  if (os.windows && os.windows.min_kernel_version == "")
    os.windows.min_kernel_version = "0";
  if (os.linux && os.linux.min_kernel_version == "")
    os.linux.min_kernel_version = "0";
  if (os.ios && os.ios.min_version == "") os.ios.min_version = "0";
  return os;
};

export const validateScheduleCheck = (
  check?: ScheduleCheck,
): ScheduleCheck | undefined => {
  if (!check) return undefined;
  if (!check.window) return undefined;
  if (!HHMM.test(check.window.start_time)) return undefined;
  if (!HHMM.test(check.window.end_time)) return undefined;
  if (check.action !== "allow" && check.action !== "deny") return undefined;

  const days = check.window.days_of_week;
  const normalisedDays =
    Array.isArray(days) && days.length > 0
      ? Array.from(new Set(days))
          .filter((d) => Number.isInteger(d) && d >= 0 && d <= 6)
          .sort((a, b) => a - b)
      : undefined;

  const tz = (check.timezone ?? "").trim();
  const timezone = tz === "" || tz === "UTC" ? undefined : tz;

  return {
    window: {
      start_time: check.window.start_time,
      end_time: check.window.end_time,
      ...(normalisedDays ? { days_of_week: normalisedDays } : {}),
    },
    ...(timezone ? { timezone } : {}),
    action: check.action,
  };
};

export const validateLocationCheck = (locationCheck?: GeoLocationCheck) => {
  if (!locationCheck) return;
  if (!locationCheck.locations) return;
  return {
    action: locationCheck.action,
    locations: locationCheck.locations.map((location) => {
      if (location.city_name == "")
        return { country_code: location.country_code } as GeoLocation;
      return {
        country_code: location.country_code,
        city_name: location.city_name,
      } as GeoLocation;
    }),
  } as GeoLocationCheck;
};
