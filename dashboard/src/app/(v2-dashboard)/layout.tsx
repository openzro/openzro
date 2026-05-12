import V2DashboardLayout from "@/layouts/V2DashboardLayout";

// (v2-dashboard) is the route group for screens migrated to the
// Notion/Arc-flavored chrome introduced by ADR-0016 phase 4. Group
// markers are URL-invisible: a route at (v2-dashboard)/peers still
// resolves to /peers. The legacy (dashboard) group keeps its
// DashboardLayout chrome for routes that haven't been migrated yet.
//
// Conflict rule: a given URL path may live in only one route group.
// When migrating a screen, MOVE its directory from (dashboard) to
// (v2-dashboard) — do not duplicate.
export default V2DashboardLayout;
