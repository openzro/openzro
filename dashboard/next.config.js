/** @type {import('next').NextConfig} */

// Production builds are static-exported (the dashboard ships as
// pre-rendered HTML/JS served by the nginx+management pair —
// nginx routes /login, /auth/, /setup, /api to management; the
// rest stays with the dashboard).
//
// Local `next dev` is a different beast: contributors hit the
// dashboard at http://localhost:3000 while the management binary
// is on http://localhost:33071. Without rewrites the browser
// would have to talk to two origins, which (a) breaks the
// oz_session cookie (cravado em :33071, não chega em :3000) and
// (b) needs CORS the prod path doesn't have. The dev rewrites
// below let the browser see everything as same-origin :3000 and
// the cookie flows naturally — same UX shape as prod.
const isDev = process.env.NODE_ENV === "development";
const MGMT_DEV_TARGET =
  process.env.OPENZRO_MGMT_DEV_TARGET || "http://localhost:33071";

const nextConfig = {
  // output: "export" is incompatible with `rewrites`. Keep the
  // static-export build for prod; skip it in dev so rewrites
  // are honoured by `next dev`.
  ...(isDev ? {} : { output: "export" }),
  images: {
    unoptimized: true,
  },
  reactStrictMode: false,
  env: {
    APP_ENV: process.env.APP_ENV || "production",
  },
  ...(isDev && {
    async rewrites() {
      return [
        // Each path the management owns in production. Order
        // doesn't matter — Next picks the first match and these
        // don't collide with any dashboard route.
        { source: "/login", destination: `${MGMT_DEV_TARGET}/login` },
        { source: "/setup", destination: `${MGMT_DEV_TARGET}/setup` },
        { source: "/auth/:path*", destination: `${MGMT_DEV_TARGET}/auth/:path*` },
        { source: "/api/:path*", destination: `${MGMT_DEV_TARGET}/api/:path*` },
      ];
    },
  }),
};

module.exports = nextConfig;
