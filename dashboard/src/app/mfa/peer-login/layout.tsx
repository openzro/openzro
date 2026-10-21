import { globalMetaTitle } from "@utils/meta";
import type { Metadata } from "next";

// The page carries an approval token in memory and a code in a form, so it
// sends no referrer to anything it loads.
export const metadata: Metadata = {
  title: `Approve sign-in - ${globalMetaTitle}`,
  referrer: "no-referrer",
};

export default function PeerLoginLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return children;
}
