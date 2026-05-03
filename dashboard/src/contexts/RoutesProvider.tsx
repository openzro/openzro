import { notify } from "@components/Notification";
import { useApiCall } from "@utils/api";
import React from "react";
import { useSWRConfig } from "swr";
import { GroupedRoute, Route } from "@/interfaces/Route";

type Props = {
  children: React.ReactNode;
};

// Fields the Edit-Group UI is allowed to mass-apply to every child
// route in a GroupedRoute. Peer assignment, masquerade, and groups
// stay per-peer (edited via the inner row's RouteUpdateModal).
export type GroupedRouteUpdateFields = {
  network_id?: string;
  description?: string;
  enabled?: boolean;
  metric?: number;
  network?: string;
  domains?: string[];
};

const RoutesContext = React.createContext(
  {} as {
    createRoute: (
      route: Route,
      onSuccess?: (route: Route) => void,
      message?: string,
    ) => void;
    updateRoute: (
      route: Route,
      toUpdate: Partial<Route>,
      onSuccess?: (route: Route) => void,
      message?: string,
      options?: { remove_access_control_groups?: boolean },
    ) => void;
    updateGroupedRoute: (
      grouped: GroupedRoute,
      toUpdate: GroupedRouteUpdateFields,
      onSuccess?: () => void,
    ) => void;
  },
);

export default function RoutesProvider({ children }: Readonly<Props>) {
  const routeRequest = useApiCall<Route>("/routes", true);
  const { mutate } = useSWRConfig();

  const updateRoute = async (
    route: Route,
    toUpdate: Partial<Route>,
    onSuccess?: (route: Route) => void,
    message?: string,
    options?: { remove_access_control_groups?: boolean },
  ) => {
    const hasDomains = route.domains ? route.domains.length > 0 : false;

    notify({
      title: "Network " + route.network_id + "-" + route.network,
      description: message ?? "The network route was successfully updated",
      promise: routeRequest
        .put(
          {
            network_id: route.network_id,
            description: toUpdate.description ?? route.description ?? "",
            enabled: toUpdate.enabled ?? route.enabled,
            peer: toUpdate.peer ?? (route.peer || undefined),
            peer_groups:
              toUpdate.peer_groups ?? (route.peer_groups || undefined),
            network: !hasDomains ? route.network : undefined,
            domains: hasDomains ? route.domains : undefined,
            keep_route: route.keep_route,
            metric: toUpdate.metric ?? route.metric ?? 9999,
            masquerade: toUpdate.masquerade ?? route.masquerade ?? true,
            groups: toUpdate.groups ?? route.groups ?? [],
            access_control_groups: options?.remove_access_control_groups
              ? undefined
              : toUpdate.access_control_groups ??
                route.access_control_groups ??
                undefined,
          },
          `/${route.id}`,
        )
        .then((route) => {
          mutate("/groups");
          onSuccess && onSuccess(route);
        }),
      loadingMessage: "Updating route...",
    });
  };

  const createRoute = async (
    route: Route,
    onSuccess?: (route: Route) => void,
    message?: string,
  ) => {
    notify({
      title: "Network " + route.network_id + "-" + route.network,
      description: message ?? "The network route was successfully created",
      promise: routeRequest
        .post({
          network_id: route.network_id,
          description: route.description || "",
          enabled: route.enabled,
          peer: route.peer || undefined,
          peer_groups: route.peer_groups || undefined,
          network: route?.network || undefined,
          domains: route?.domains || undefined,
          keep_route: route?.keep_route || false,
          metric: route.metric || 9999,
          masquerade: route.masquerade,
          groups: route.groups || [],
          access_control_groups: route?.access_control_groups || undefined,
        })
        .then((route) => {
          mutate("/routes");
          mutate("/groups");
          onSuccess && onSuccess(route);
        }),
      loadingMessage: "Creating route...",
    });
  };

  // Group-level edit: fans out N PUTs (one per child route) so a
  // single user action ("change this network's CIDR / description /
  // enabled / metric") propagates to every peer attached to the
  // grouped route. Each PUT preserves the child's per-peer fields
  // (peer / peer_groups / masquerade / groups / access_control_groups)
  // and applies the shared fields from `toUpdate`. We use Promise.all
  // — fail-fast — because partial success would split the group on
  // the listing and is harder to reason about than a clean retry.
  const updateGroupedRoute = async (
    grouped: GroupedRoute,
    toUpdate: GroupedRouteUpdateFields,
    onSuccess?: () => void,
  ) => {
    if (!grouped.routes || grouped.routes.length === 0) return;

    const groupHasDomains =
      toUpdate.domains !== undefined
        ? toUpdate.domains.length > 0
        : grouped.domains
        ? grouped.domains.length > 0
        : false;

    const batch = grouped.routes
      .filter((r) => r.id)
      .map((r) => {
        const childHasDomains = r.domains ? r.domains.length > 0 : false;
        const isDomain = groupHasDomains || childHasDomains;
        return routeRequest.put(
          {
            network_id: toUpdate.network_id ?? r.network_id,
            description: toUpdate.description ?? r.description ?? "",
            enabled: toUpdate.enabled ?? r.enabled,
            peer: r.peer || undefined,
            peer_groups: r.peer_groups || undefined,
            network: !isDomain
              ? toUpdate.network ?? r.network
              : undefined,
            domains: isDomain
              ? toUpdate.domains ?? r.domains
              : undefined,
            keep_route: r.keep_route,
            metric: toUpdate.metric ?? r.metric ?? 9999,
            masquerade: r.masquerade ?? true,
            groups: r.groups ?? [],
            access_control_groups: r.access_control_groups ?? undefined,
          },
          `/${r.id}`,
        );
      });

    notify({
      title: "Network " + grouped.network_id,
      description: "The network was successfully updated",
      promise: Promise.all(batch).then(() => {
        mutate("/routes");
        mutate("/groups");
        onSuccess && onSuccess();
      }),
      loadingMessage: "Updating the network...",
    });
  };

  return (
    <RoutesContext.Provider
      value={{ createRoute, updateRoute, updateGroupedRoute }}
    >
      {children}
    </RoutesContext.Provider>
  );
}

export const useRoutes = () => {
  return React.useContext(RoutesContext);
};
