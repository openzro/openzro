import useFetchApi from "@utils/api";

interface LocalUserConfig {
  creation_enabled: boolean;
}

// useLocalUserConfig reports whether the server lets the console create
// local users (ADR-0026 D1). Errors are ignored so an older management
// server without the endpoint simply hides the action.
export default function useLocalUserConfig(allowFetch = true) {
  const { data } = useFetchApi<LocalUserConfig>(
    "/users/local/config",
    true,
    false,
    allowFetch,
  );
  return { creationEnabled: data?.creation_enabled === true };
}
