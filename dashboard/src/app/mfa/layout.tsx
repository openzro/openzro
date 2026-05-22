// Layout for the public-style /mfa/* routes that the auth middleware
// redirects to when an MFA challenge or enrollment is required.
// Keeps the page chrome-free: no sidebar, no topbar — these screens
// are interstitials between OIDC login and the real dashboard.

export default function MFALayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <div className="min-h-screen bg-oz2-bg-canvas">
      <main className="mx-auto flex min-h-screen max-w-[440px] flex-col items-center justify-center px-4 py-10">
        {children}
      </main>
    </div>
  );
}
